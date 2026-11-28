// Package coinbase is a raw Coinbase Advanced Trade market-data WebSocket client.
//
// It connects without authentication, sends one subscribe message per channel
// for the configured products, and hands every received message to a Handler
// unmodified. Connection handling (dial, read limit, idle timeout, backoff,
// reconnect) is done by package wsfeed; on every new connection sequence
// tracking is reset.
package coinbase

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/Vitaliy-Pikalo/market-data-platform/internal/backoff"
	"github.com/Vitaliy-Pikalo/market-data-platform/internal/seqcheck"
	"github.com/Vitaliy-Pikalo/market-data-platform/internal/wsfeed"
)

// Defaults.
const (
	DefaultURL = "wss://advanced-trade-ws.coinbase.com"

	// DefaultReadLimit is the largest single message accepted. level2
	// snapshots are far above coder/websocket's 32 KiB default.
	DefaultReadLimit = wsfeed.DefaultReadLimit

	// DefaultIdleTimeout: with the heartbeats channel subscribed, Coinbase
	// sends a message every second, so silence this long means a dead connection.
	DefaultIdleTimeout = wsfeed.DefaultIdleTimeout

	DefaultDialTimeout = wsfeed.DefaultDialTimeout
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
	// Dropped is called for a message that is not valid JSON. Such messages
	// cannot be stored as raw JSON and are not passed to Message.
	Dropped func(raw []byte, err error)
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

// Run connects and streams until ctx is cancelled (returns nil) or
// Handler.Message returns an error (returns that error). Connection errors
// are retried forever.
func (c *Client) Run(ctx context.Context, h Handler) error {
	if h.Message == nil {
		return errors.New("coinbase: Handler.Message is required")
	}
	subs := make([]wsfeed.Subscription, 0, len(c.cfg.Channels))
	for _, ch := range c.cfg.Channels {
		msg, _ := json.Marshal(subscribe{Type: "subscribe", ProductIDs: c.cfg.Products, Channel: ch})
		subs = append(subs, wsfeed.Subscription{Name: ch, Msg: msg})
	}
	return wsfeed.Run(ctx, wsfeed.Config{
		URL:          c.cfg.URL,
		ReadLimit:    c.cfg.ReadLimit,
		IdleTimeout:  c.cfg.IdleTimeout,
		DialTimeout:  c.cfg.DialTimeout,
		Backoff:      c.cfg.Backoff,
		Logger:       c.log,
		ConnectAttrs: []any{"products", c.cfg.Products, "channels", c.cfg.Channels},
	}, wsfeed.Session{
		Subscriptions: subs,
		OnConnect:     c.seq.Reset,
		OnMessage: func(recv time.Time, data []byte) error {
			return c.handle(h, recv, data)
		},
		OnReconnect: h.Reconnect,
	})
}

// handle processes one message. A returned error comes from Handler.Message.
func (c *Client) handle(h Handler, recv time.Time, data []byte) error {
	var hdr header
	if err := json.Unmarshal(data, &hdr); err != nil {
		// Not JSON: cannot be stored as raw JSON. Log and skip.
		c.log.Error("invalid json message dropped", "err", err.Error(), "len", len(data))
		if h.Dropped != nil {
			h.Dropped(data, err)
		}
		return nil
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
	return h.Message(Message{RecvNs: recv.UnixNano(), Channel: hdr.Channel, Raw: data})
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
