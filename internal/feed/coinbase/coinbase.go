// Package coinbase is a raw Coinbase Advanced Trade market-data WebSocket client.
//
// It connects without authentication, sends one subscribe message per channel
// for the configured products, and hands every received message to a Handler
// unmodified. On any connection error it reconnects with exponential backoff
// and resets sequence tracking.
package coinbase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/coder/websocket"

	"github.com/Vitaliy-Pikalo/market-data-platform/internal/backoff"
	"github.com/Vitaliy-Pikalo/market-data-platform/internal/seqcheck"
)

// Defaults.
const (
	DefaultURL = "wss://advanced-trade-ws.coinbase.com"

	// DefaultReadLimit is the largest single message accepted. level2
	// snapshots are far above coder/websocket's 32 KiB default.
	DefaultReadLimit = 64 << 20 // 64 MiB

	// DefaultIdleTimeout: with the heartbeats channel subscribed, Coinbase
	// sends a message every second, so silence this long means a dead connection.
	DefaultIdleTimeout = 30 * time.Second

	DefaultDialTimeout = 15 * time.Second
)

// DefaultChannels are subscribed to when Config.Channels is empty.
var DefaultChannels = []string{"market_trades", "level2", "heartbeats"}

// Config configures a Client. Zero values take the defaults above.
type Config struct {
	URL         string
	Products    []string
	Channels    []string
	ReadLimit   int64
	IdleTimeout time.Duration
	DialTimeout time.Duration
	Backoff     *backoff.Backoff
	Logger      *slog.Logger
}

// Message is one received WebSocket message.
type Message struct {
	RecvNs  int64  // time.Now().UnixNano() right after the read returned
	Channel string // "channel" field of the message, e.g. "l2_data"; "" if absent
	Raw     []byte // the message exactly as received
}

// Handler receives events from Run. Message is required; the others may be nil.
// All callbacks run on the Run goroutine.
type Handler struct {
	// Message is called for every message. A non-nil error stops Run and is returned from it.
	Message func(Message) error
	// Gap is called when sequence_num skips or goes backwards.
	Gap func(channel string, r seqcheck.Result)
	// Reconnect is called before each reconnect wait.
	Reconnect func(attempt int, wait time.Duration, cause error)
}

// Client connects to Coinbase and streams messages to a Handler.
type Client struct {
	cfg Config
	log *slog.Logger
	seq *seqcheck.Checker
}

// New returns a Client. It does not connect until Run is called.
func New(cfg Config) *Client {
	if cfg.URL == "" {
		cfg.URL = DefaultURL
	}
	if len(cfg.Channels) == 0 {
		cfg.Channels = DefaultChannels
	}
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
	return &Client{cfg: cfg, log: cfg.Logger.With("src", "coinbase"), seq: seqcheck.New()}
}

// handlerError wraps an error returned by Handler.Message so Run can tell it
// apart from connection errors.
type handlerError struct{ err error }

func (e handlerError) Error() string { return e.err.Error() }
func (e handlerError) Unwrap() error { return e.err }

// Run connects and streams until ctx is cancelled (returns nil) or
// Handler.Message returns an error (returns that error). Connection errors
// are retried forever.
func (c *Client) Run(ctx context.Context, h Handler) error {
	if h.Message == nil {
		return errors.New("coinbase: Handler.Message is required")
	}
	for {
		uptime, err := c.session(ctx, h)
		if ctx.Err() != nil {
			return nil
		}
		var he handlerError
		if errors.As(err, &he) {
			return he.err
		}
		wait := c.cfg.Backoff.Next(uptime)
		attempt := c.cfg.Backoff.Attempt()
		c.log.Warn("reconnecting", "attempt", attempt, "wait", wait.String(),
			"uptime", uptime.Round(time.Millisecond).String(), "err", errString(err))
		if h.Reconnect != nil {
			h.Reconnect(attempt, wait, err)
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

// session runs one connection. It returns how long the connection was up
// (0 if it never got past subscribing) and why it ended.
func (c *Client) session(ctx context.Context, h Handler) (time.Duration, error) {
	dialCtx, cancel := context.WithTimeout(ctx, c.cfg.DialTimeout)
	conn, _, err := websocket.Dial(dialCtx, c.cfg.URL, nil)
	cancel()
	if err != nil {
		return 0, fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(c.cfg.ReadLimit)

	for _, ch := range c.cfg.Channels {
		sub, _ := json.Marshal(subscribe{Type: "subscribe", ProductIDs: c.cfg.Products, Channel: ch})
		wctx, cancel := context.WithTimeout(ctx, c.cfg.DialTimeout)
		err := conn.Write(wctx, websocket.MessageText, sub)
		cancel()
		if err != nil {
			return 0, fmt.Errorf("subscribe %s: %w", ch, err)
		}
	}
	c.seq.Reset()
	start := time.Now()
	c.log.Info("connected", "url", c.cfg.URL, "products", c.cfg.Products, "channels", c.cfg.Channels)

	for {
		rctx, cancel := context.WithTimeout(ctx, c.cfg.IdleTimeout)
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

		var hdr header
		if err := json.Unmarshal(data, &hdr); err != nil {
			// Not JSON: cannot be stored as raw JSON. Log and skip.
			c.log.Error("invalid json message dropped", "err", err.Error(), "len", len(data))
			continue
		}
		if hdr.Type == "error" {
			c.log.Error("server error message", "message", hdr.Message)
		}
		if hdr.Seq != nil {
			if r := c.seq.Observe(*hdr.Seq); r.Kind != seqcheck.OK {
				c.log.Warn("sequence "+r.Kind.String(), "channel", hdr.Channel,
					"expected", r.Expected, "got", r.Got, "missing", r.Missing)
				if h.Gap != nil {
					h.Gap(hdr.Channel, r)
				}
			}
		}
		if err := h.Message(Message{RecvNs: recv.UnixNano(), Channel: hdr.Channel, Raw: data}); err != nil {
			conn.Close(websocket.StatusNormalClosure, "shutdown")
			return time.Since(start), handlerError{err}
		}
	}
}

type subscribe struct {
	Type       string   `json:"type"`
	ProductIDs []string `json:"product_ids,omitempty"`
	Channel    string   `json:"channel"`
}

// header holds the top-level fields the client inspects.
type header struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
	Message string `json:"message"`
	Seq     *int64 `json:"sequence_num"`
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
