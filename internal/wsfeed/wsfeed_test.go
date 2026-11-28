package wsfeed

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Vitaliy-Pikalo/market-data-platform/internal/backoff"
)

// server is a local WebSocket server. Each connection first reads wantSubs
// subscription messages, then sends msgs and closes (if closeAfter) or waits
// for the client to go away.
type server struct {
	wantSubs   int
	msgs       []string
	closeAfter bool

	mu    sync.Mutex
	conns int
	subs  []string
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	s.mu.Lock()
	s.conns++
	s.mu.Unlock()
	ctx := r.Context()
	// Read the subscriptions before sending anything, so they are recorded
	// even when the server closes right after sending.
	for i := 0; i < s.wantSubs; i++ {
		_, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		s.mu.Lock()
		s.subs = append(s.subs, string(b))
		s.mu.Unlock()
	}
	go func() {
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()
	for _, m := range s.msgs {
		if c.Write(ctx, websocket.MessageText, []byte(m)) != nil {
			return
		}
	}
	if s.closeAfter {
		c.Close(websocket.StatusGoingAway, "bye")
		return
	}
	<-ctx.Done()
}

func wsURL(ts *httptest.Server) string { return "ws" + strings.TrimPrefix(ts.URL, "http") }

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func fastBackoff() *backoff.Backoff {
	b := backoff.New(time.Millisecond, time.Millisecond, time.Hour)
	b.Rand = func(int64) int64 { return 0 }
	return b
}

func TestRun(t *testing.T) {
	errFatal := errors.New("fatal")
	tests := []struct {
		name          string
		srv           *server
		stopOn        string // OnMessage returns errFatal for this message
		cancelAfter   int    // cancel ctx after this many messages (0 = never)
		wantErr       error
		wantMsgs      []string
		wantConns     int // server-side connection count (minimum)
		wantReconnect int // OnReconnect calls (minimum)
		wantConnects  int // OnConnect calls (minimum)
	}{
		{
			name:         "OnMessage error stops Run without reconnect",
			srv:          &server{wantSubs: 2, msgs: []string{"a", "b", "c"}},
			stopOn:       "b",
			wantErr:      errFatal,
			wantMsgs:     []string{"a", "b"},
			wantConns:    1,
			wantConnects: 1,
		},
		{
			name:          "server close reconnects and resubscribes",
			srv:           &server{wantSubs: 2, msgs: []string{"x"}, closeAfter: true},
			cancelAfter:   3,
			wantMsgs:      []string{"x", "x", "x"},
			wantConns:     3,
			wantReconnect: 2,
			wantConnects:  3,
		},
		{
			name:         "context cancel returns nil",
			srv:          &server{wantSubs: 2, msgs: []string{"a"}},
			cancelAfter:  1,
			wantMsgs:     []string{"a"},
			wantConns:    1,
			wantConnects: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(tt.srv)
			defer ts.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			var got []string
			var reconnects, connects int
			err := Run(ctx, Config{URL: wsURL(ts), Backoff: fastBackoff(), Logger: quietLogger()}, Session{
				Subscriptions: []Subscription{{Name: "s1", Msg: []byte(`sub1`)}, {Name: "s2", Msg: []byte(`sub2`)}},
				OnConnect:     func() { connects++ },
				OnMessage: func(_ time.Time, data []byte) error {
					got = append(got, string(data))
					if string(data) == tt.stopOn {
						return errFatal
					}
					if tt.cancelAfter > 0 && len(got) >= tt.cancelAfter {
						cancel()
					}
					return nil
				},
				OnReconnect: func(int, time.Duration, error) { reconnects++ },
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Run err = %v, want %v", err, tt.wantErr)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantMsgs, ",") {
				t.Errorf("messages = %v, want %v", got, tt.wantMsgs)
			}
			if reconnects < tt.wantReconnect || (tt.wantReconnect == 0 && reconnects != 0) {
				t.Errorf("reconnects = %d, want %d", reconnects, tt.wantReconnect)
			}
			if connects < tt.wantConnects {
				t.Errorf("OnConnect calls = %d, want >= %d", connects, tt.wantConnects)
			}
			tt.srv.mu.Lock()
			defer tt.srv.mu.Unlock()
			if tt.srv.conns < tt.wantConns || (tt.wantReconnect == 0 && tt.srv.conns != tt.wantConns) {
				t.Errorf("server connections = %d, want %d", tt.srv.conns, tt.wantConns)
			}
			// Every connection must have sent both subscriptions, in order.
			if len(tt.srv.subs) != 2*tt.srv.conns {
				t.Fatalf("server received %d subscriptions over %d connections: %v", len(tt.srv.subs), tt.srv.conns, tt.srv.subs)
			}
			for i, s := range tt.srv.subs {
				if want := []string{"sub1", "sub2"}[i%2]; s != want {
					t.Errorf("subscription %d = %q, want %q (all: %v)", i, s, want, tt.srv.subs)
				}
			}
		})
	}
}

func TestIdleTimeoutReconnects(t *testing.T) {
	srv := &server{msgs: []string{"only"}} // then silent, connection stays open
	ts := httptest.NewServer(srv)
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var msgs int
	var causes []error
	err := Run(ctx, Config{URL: wsURL(ts), IdleTimeout: 100 * time.Millisecond, Backoff: fastBackoff(), Logger: quietLogger()}, Session{
		OnMessage: func(time.Time, []byte) error {
			msgs++
			if msgs == 2 {
				cancel()
			}
			return nil
		},
		OnReconnect: func(_ int, _ time.Duration, cause error) { causes = append(causes, cause) },
	})
	if err != nil {
		t.Fatalf("Run err = %v", err)
	}
	if len(causes) < 1 || !strings.HasPrefix(causes[0].Error(), "read:") {
		t.Fatalf("reconnect causes = %v, want a read timeout first", causes)
	}
}
