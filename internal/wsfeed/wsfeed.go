// Package wsfeed runs a reconnecting WebSocket session for an exchange feed.
//
// It owns the parts every feed shares: dial with timeout, a raised read
// limit, sending subscribe messages, an idle (read) timeout that detects dead
// connections, and reconnecting with exponential backoff. Exchange packages
// supply the subscribe messages and handle each received message.
package wsfeed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/coder/websocket"

	"github.com/Vitaliy-Pikalo/market-data-platform/internal/backoff"
)

// Defaults for zero Config fields.
const (
	// DefaultReadLimit is the largest single message accepted. Order book
	// snapshots are far above coder/websocket's 32 KiB default.
	DefaultReadLimit = 64 << 20 // 64 MiB

	// DefaultIdleTimeout: a connection with no message for this long is
	// treated as dead and reconnected.
	DefaultIdleTimeout = 30 * time.Second

	DefaultDialTimeout = 15 * time.Second
)

// Config configures Run. Zero values take the defaults above.
type Config struct {
	URL         string
	ReadLimit   int64
	IdleTimeout time.Duration
	DialTimeout time.Duration // also bounds each subscribe write
	Backoff     *backoff.Backoff
	Logger      *slog.Logger // used as is; add the feed name with Logger.With
	// ConnectAttrs are extra key/value pairs logged with "connected".
	ConnectAttrs []any
}

// Subscription is one message sent right after connecting.
type Subscription struct {
	Name string // for error messages, e.g. the channel
	Msg  []byte
}

// Session is what a feed plugs into Run. OnMessage is required.
type Session struct {
	// Subscriptions are sent in order on every new connection.
	Subscriptions []Subscription
	// OnConnect is called after the subscriptions were sent, before the first read.
	// Use it to reset per-connection state.
	OnConnect func()
	// OnMessage is called for every received message with the time the read
	// returned. A non-nil error stops Run, which returns that error; it is not
	// retried. Use it for errors a reconnect cannot fix.
	OnMessage func(recv time.Time, data []byte) error
	// OnReconnect is called before each reconnect wait.
	OnReconnect func(attempt int, wait time.Duration, cause error)
}

// stopError wraps an error returned by OnMessage so Run can tell it apart
// from connection errors.
type stopError struct{ err error }

func (e stopError) Error() string { return e.err.Error() }
func (e stopError) Unwrap() error { return e.err }

// Run connects and streams until ctx is cancelled (returns nil) or OnMessage
// returns an error (returns that error). Connection errors are retried forever.
func Run(ctx context.Context, cfg Config, s Session) error {
	if s.OnMessage == nil {
		return errors.New("wsfeed: Session.OnMessage is required")
	}
	cfg = withDefaults(cfg)
	for {
		uptime, err := session(ctx, cfg, s)
		if ctx.Err() != nil {
			return nil
		}
		var se stopError
		if errors.As(err, &se) {
			return se.err
		}
		wait := cfg.Backoff.Next(uptime)
		attempt := cfg.Backoff.Attempt()
		cfg.Logger.Warn("reconnecting", "attempt", attempt, "wait", wait.String(),
			"uptime", uptime.Round(time.Millisecond).String(), "err", errString(err))
		if s.OnReconnect != nil {
			s.OnReconnect(attempt, wait, err)
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-t.C:
		}
	}
}

func withDefaults(cfg Config) Config {
	if cfg.ReadLimit == 0 {
		cfg.ReadLimit = DefaultReadLimit
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = DefaultDialTimeout
	}
	if cfg.Backoff == nil {
		cfg.Backoff = backoff.New(backoff.DefaultBase, backoff.DefaultMax, backoff.DefaultResetAfter)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return cfg
}

// session runs one connection. It returns how long the connection was up
// (0 if it never got past subscribing) and why it ended.
func session(ctx context.Context, cfg Config, s Session) (time.Duration, error) {
	dialCtx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
	conn, _, err := websocket.Dial(dialCtx, cfg.URL, nil)
	cancel()
	if err != nil {
		return 0, fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(cfg.ReadLimit)

	for _, sub := range s.Subscriptions {
		wctx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
		err := conn.Write(wctx, websocket.MessageText, sub.Msg)
		cancel()
		if err != nil {
			return 0, fmt.Errorf("subscribe %s: %w", sub.Name, err)
		}
	}
	if s.OnConnect != nil {
		s.OnConnect()
	}
	start := time.Now()
	cfg.Logger.Info("connected", append([]any{"url", cfg.URL}, cfg.ConnectAttrs...)...)

	for {
		rctx, cancel := context.WithTimeout(ctx, cfg.IdleTimeout)
		_, data, err := conn.Read(rctx)
		recv := time.Now()
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				conn.Close(websocket.StatusNormalClosure, "shutdown")
				return time.Since(start), ctx.Err()
			}
			return time.Since(start), fmt.Errorf("read: %w", err)
		}
		if err := s.OnMessage(recv, data); err != nil {
			conn.Close(websocket.StatusNormalClosure, "shutdown")
			return time.Since(start), stopError{err}
		}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
