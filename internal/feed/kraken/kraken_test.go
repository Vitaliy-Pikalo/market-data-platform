package kraken

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return m
}

func TestSubscribeMessagesEncoding(t *testing.T) {
	subs := SubscribeMessages([]string{"BTC/USD", "ETH/USD", "SOL/USD"}, 10)
	want := []struct{ name, json string }{
		{"trade", `{"method":"subscribe","params":{"channel":"trade","symbol":["BTC/USD","ETH/USD","SOL/USD"],"snapshot":true},"req_id":1}`},
		{"book", `{"method":"subscribe","params":{"channel":"book","symbol":["BTC/USD","ETH/USD","SOL/USD"],"depth":10,"snapshot":true},"req_id":2}`},
	}
	if len(subs) != len(want) {
		t.Fatalf("got %d subscribe messages, want %d", len(subs), len(want))
	}
	for i, w := range want {
		if subs[i].Name != w.name || string(subs[i].Msg) != w.json {
			t.Errorf("subscription %d = %s %s\nwant           %s %s", i, subs[i].Name, subs[i].Msg, w.name, w.json)
		}
	}
}

// Our requests must say the same as the doc examples. The only differences
// allowed are the ones listed: req_id (ours, optional per docs) and book
// parameters we set explicitly that the doc example leaves at their documented
// defaults (depth 10, snapshot true).
func TestSubscribeMessagesMatchDocExamples(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		symbols  []string
		index    int            // 0 trade, 1 book
		addToDoc map[string]any // params we send in addition to the doc example
	}{
		{"trade", "doc_trade_subscribe_request.json", []string{"MATIC/USD"}, 0, nil},
		{"book", "doc_book_subscribe_request.json", []string{"ALGO/USD", "MATIC/USD"}, 1, map[string]any{"depth": 10.0, "snapshot": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := decode(t, fixture(t, tt.fixture))
			for k, v := range tt.addToDoc {
				doc["params"].(map[string]any)[k] = v
			}
			doc["req_id"] = float64(tt.index + 1)
			ours := decode(t, SubscribeMessages(tt.symbols, 10)[tt.index].Msg)
			if !reflect.DeepEqual(ours, doc) {
				t.Errorf("ours = %v\nwant (doc example + listed differences) = %v", ours, doc)
			}
		})
	}
}

func TestParseHeader(t *testing.T) {
	tests := []struct {
		fixture     string
		wantKey     string
		wantType    string
		wantSuccess *bool
		wantChannel string // SubscribeTarget channel, for subscribe responses
		wantSymbol  string // SubscribeTarget symbol
		wantErr     string
	}{
		{fixture: "doc_trade_snapshot.json", wantKey: "trade", wantType: "snapshot"},
		{fixture: "doc_trade_update.json", wantKey: "trade", wantType: "update"},
		{fixture: "doc_book_snapshot.json", wantKey: "book", wantType: "snapshot"},
		{fixture: "doc_book_update.json", wantKey: "book", wantType: "update"},
		{fixture: "doc_heartbeat.json", wantKey: "heartbeat"},
		{fixture: "doc_status_update.json", wantKey: "status", wantType: "update"},
		{fixture: "doc_pong.json", wantKey: "pong"},
		{fixture: "doc_trade_subscribe_response.json", wantKey: "subscribe", wantSuccess: ptr(true), wantChannel: "trade", wantSymbol: "MATIC/USD"},
		{fixture: "doc_book_subscribe_response_1.json", wantKey: "subscribe", wantSuccess: ptr(true), wantChannel: "book", wantSymbol: "ALGO/USD"},
		{fixture: "doc_book_subscribe_response_2.json", wantKey: "subscribe", wantSuccess: ptr(true), wantChannel: "book", wantSymbol: "MATIC/USD"},
		{
			fixture: "live_subscribe_rejected.json", wantKey: "subscribe", wantSuccess: ptr(false),
			wantChannel: "trade", wantSymbol: "FOO/BAR", wantErr: "Currency pair not supported FOO/BAR",
		},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			h, err := ParseHeader(fixture(t, tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if h.Key() != tt.wantKey || h.Type != tt.wantType {
				t.Errorf("Key/Type = %q/%q, want %q/%q", h.Key(), h.Type, tt.wantKey, tt.wantType)
			}
			if !reflect.DeepEqual(h.Success, tt.wantSuccess) {
				t.Errorf("Success = %v, want %v", deref(h.Success), deref(tt.wantSuccess))
			}
			if h.Method == "subscribe" {
				ch, sym := h.SubscribeTarget()
				if ch != tt.wantChannel || sym != tt.wantSymbol {
					t.Errorf("SubscribeTarget = %q %q, want %q %q", ch, sym, tt.wantChannel, tt.wantSymbol)
				}
			}
			if h.Error != tt.wantErr {
				t.Errorf("Error = %q, want %q", h.Error, tt.wantErr)
			}
		})
	}
}

func TestParseHeaderInvalid(t *testing.T) {
	for _, in := range []string{``, `not json`, `{"channel":`, `[1,2]`} {
		if _, err := ParseHeader([]byte(in)); err == nil {
			t.Errorf("ParseHeader(%q) = nil error, want error", in)
		}
	}
	// A valid object whose result has an unexpected shape is still accepted.
	h, err := ParseHeader([]byte(`{"method":"subscribe","result":[1],"success":true}`))
	if err != nil || h.Key() != "subscribe" {
		t.Errorf("unexpected result shape: %+v, %v", h, err)
	}
}

func TestHandle(t *testing.T) {
	tests := []struct {
		name        string
		fixture     string
		raw         string // used when fixture is empty
		wantMsg     bool   // Handler.Message called
		wantDropped bool
		wantSubErr  *SubscribeError
	}{
		{name: "book update passes through", fixture: "doc_book_update.json", wantMsg: true},
		{name: "successful ack passes through", fixture: "doc_book_subscribe_response_1.json", wantMsg: true},
		{
			name: "rejected ack is recorded, then stops Run", fixture: "live_subscribe_rejected.json", wantMsg: true,
			wantSubErr: &SubscribeError{Channel: "trade", Symbol: "FOO/BAR", Err: "Currency pair not supported FOO/BAR"},
		},
		{name: "non-json is dropped", raw: "garbage", wantDropped: true},
	}
	c := New(Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(tt.raw)
			if tt.fixture != "" {
				data = fixture(t, tt.fixture)
			}
			var got *Message
			var dropped bool
			h := Handler{
				Message: func(m Message) error { got = &m; return nil },
				Dropped: func([]byte, error) { dropped = true },
			}
			recv := time.Unix(0, 1795722796662522500)
			err := c.handle(h, recv, data)

			if (got != nil) != tt.wantMsg {
				t.Fatalf("Message called = %v, want %v", got != nil, tt.wantMsg)
			}
			if got != nil && (got.RecvNs != recv.UnixNano() || string(got.Raw) != string(data)) {
				t.Errorf("Message = %+v, want RecvNs %d and raw bytes unchanged", got, recv.UnixNano())
			}
			if dropped != tt.wantDropped {
				t.Errorf("Dropped called = %v, want %v", dropped, tt.wantDropped)
			}
			var se *SubscribeError
			if tt.wantSubErr == nil {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
			} else if !errors.As(err, &se) || *se != *tt.wantSubErr {
				t.Errorf("err = %v, want %v", err, tt.wantSubErr)
			}
		})
	}
}

func deref(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}
