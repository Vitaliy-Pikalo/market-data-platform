// Package kraken is a raw Kraken WebSocket v2 public market-data client.
//
// It subscribes to the trade and book channels for the configured symbols and
// hands every received message to a Handler unmodified. Connection handling
// (dial, read limit, idle timeout, backoff, reconnect) is done by package
// wsfeed.
//
// Protocol facts this client relies on (Kraken WebSocket v2 docs,
// https://docs.kraken.com/exchange/api-reference/spot-websocket-v2/):
//   - trade.md: subscribe params channel "trade", symbol []string, snapshot
//     (default false); the snapshot is the most recent 50 trades. Updates have
//     channel, type ("snapshot"|"update") and data; no sequence number.
//   - book.md: subscribe params channel "book", symbol []string, depth (one of
//     10, 25, 100, 500, 1000; default 10), snapshot (default true). "There is a
//     separate acknowledgement response for each symbol". Each snapshot/update
//     carries a CRC32 checksum of the top 10 levels, but no sequence number.
//   - subscribe acks: method "subscribe", result {channel, symbol, ...},
//     success bool, error string "if success is false", time_in, time_out, req_id.
//     Observed live (not in the docs): a rejected subscribe has no result; it
//     carries error, symbol and req_id at the top level, e.g.
//     {"error":"Currency pair not supported FOO/BAR","method":"subscribe","req_id":1,"success":false,"symbol":"FOO/BAR",...}
//     so the channel is recovered from req_id.
//   - heartbeat.md: {"channel":"heartbeat"} "approximately once every second in
//     the absence of any other channel updates"; there is no heartbeat
//     subscription, heartbeats "will be automatically generated on
//     subscription to any channel".
//   - status.md: a channel "status" update is sent on connect.
//   - spot-ws-intro: the server closes a connection after about a minute of
//     inactivity; Cloudflare allows about 150 (re)connection attempts per IP
//     per rolling 10 minutes.
package kraken

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Vitaliy-Pikalo/market-data-platform/internal/backoff"
	"github.com/Vitaliy-Pikalo/market-data-platform/internal/wsfeed"
)

// Defaults.
const (
	DefaultURL       = "wss://ws.kraken.com/v2"
	DefaultBookDepth = 10

	// DefaultIdleTimeout: once subscribed, Kraken sends a heartbeat about
	// every second when there is no other traffic, so 10 s of silence (about
	// 10 missed heartbeats) means a dead connection. Heartbeats need no
	// subscription of their own. This is also well below the server's own
	// ~60 s inactivity disconnect.
	DefaultIdleTimeout = 10 * time.Second
)

// DefaultSymbols are subscribed to when Config.Symbols is empty.
var DefaultSymbols = []string{"BTC/USD", "ETH/USD", "SOL/USD"}

// Config configures a Client. Zero values take the defaults.
type Config struct {
	URL         string
	Symbols     []string
	BookDepth   int
	ReadLimit   int64
	IdleTimeout time.Duration
	DialTimeout time.Duration
	Backoff     *backoff.Backoff
	Logger      *slog.Logger
}

// Message is one received WebSocket message.
type Message struct {
	RecvNs  int64  // time.Now().UnixNano() right after the read returned
	Channel string // Header.Key(): "trade", "book", "heartbeat", "status", or the method of a response such as "subscribe"
	Type    string // "snapshot", "update", or "" (heartbeat, responses)
	Raw     []byte // the message exactly as received
}

// Handler receives events from Run. Message is required; the others may be nil.
// All callbacks run on the Run goroutine.
type Handler struct {
	// Message is called for every valid JSON message, including subscribe
	// acks. A non-nil error stops Run and is returned from it.
	Message func(Message) error
	// Dropped is called for a message that is not valid JSON. Such messages
	// cannot be stored as raw JSON and are not passed to Message.
	Dropped func(raw []byte, err error)
	// Reconnect is called before each reconnect wait.
	Reconnect func(attempt int, wait time.Duration, cause error)
}

// SubscribeError is returned by Run when Kraken rejects a subscription
// (success=false in the ack). It is a configuration error, for example an
// unknown symbol or an invalid depth, so Run does not reconnect.
type SubscribeError struct {
	Channel string
	Symbol  string
	Err     string
}

func (e *SubscribeError) Error() string {
	return fmt.Sprintf("kraken: subscribe %s %s rejected: %s", e.Channel, e.Symbol, e.Err)
}

// Client connects to Kraken and streams messages to a Handler.
type Client struct {
	cfg Config
	log *slog.Logger
}

// New returns a Client. It does not connect until Run is called.
func New(cfg Config) *Client {
	if cfg.URL == "" {
		cfg.URL = DefaultURL
	}
	if len(cfg.Symbols) == 0 {
		cfg.Symbols = DefaultSymbols
	}
	if cfg.BookDepth == 0 {
		cfg.BookDepth = DefaultBookDepth
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Client{cfg: cfg, log: cfg.Logger.With("src", "kraken")}
}

// Run connects and streams until ctx is cancelled (returns nil), a
// subscription is rejected (returns *SubscribeError), or Handler.Message
// returns an error (returns that error). Connection errors are retried forever.
func (c *Client) Run(ctx context.Context, h Handler) error {
	if h.Message == nil {
		return errors.New("kraken: Handler.Message is required")
	}
	return wsfeed.Run(ctx, wsfeed.Config{
		URL:          c.cfg.URL,
		ReadLimit:    c.cfg.ReadLimit,
		IdleTimeout:  c.cfg.IdleTimeout,
		DialTimeout:  c.cfg.DialTimeout,
		Backoff:      c.cfg.Backoff,
		Logger:       c.log,
		ConnectAttrs: []any{"symbols", c.cfg.Symbols, "channels", []string{"trade", "book"}, "book_depth", c.cfg.BookDepth},
	}, wsfeed.Session{
		Subscriptions: SubscribeMessages(c.cfg.Symbols, c.cfg.BookDepth),
		OnMessage: func(recv time.Time, data []byte) error {
			return c.handle(h, recv, data)
		},
		OnReconnect: h.Reconnect,
	})
}

// handle processes one message. A returned error stops Run.
func (c *Client) handle(h Handler, recv time.Time, data []byte) error {
	hdr, err := ParseHeader(data)
	if err != nil {
		c.log.Error("invalid json message dropped", "err", err.Error(), "len", len(data))
		if h.Dropped != nil {
			h.Dropped(data, err)
		}
		return nil
	}
	// Store the message first, so a rejected ack is in the data file too.
	if err := h.Message(Message{RecvNs: recv.UnixNano(), Channel: hdr.Key(), Type: hdr.Type, Raw: data}); err != nil {
		return err
	}
	if hdr.Method == "subscribe" {
		channel, symbol := hdr.SubscribeTarget()
		if hdr.Success != nil && !*hdr.Success {
			c.log.Error("subscribe rejected", "channel", channel, "symbol", symbol, "req_id", hdr.ReqID, "error", hdr.Error)
			return &SubscribeError{Channel: channel, Symbol: symbol, Err: hdr.Error}
		}
		c.log.Info("subscribed", "channel", channel, "symbol", symbol)
	}
	return nil
}

// Request IDs of the subscribe messages, echoed back in their acks.
const (
	ReqIDTrade int64 = 1
	ReqIDBook  int64 = 2
)

// SubscribeMessages returns the subscribe requests sent on every connection:
// one for trade (with the 50-trade snapshot) and one for book at the given
// depth (snapshot is the default, true).
func SubscribeMessages(symbols []string, depth int) []wsfeed.Subscription {
	trade, _ := json.Marshal(request{Method: "subscribe", Params: params{Channel: "trade", Symbol: symbols, Snapshot: ptr(true)}, ReqID: ReqIDTrade})
	book, _ := json.Marshal(request{Method: "subscribe", Params: params{Channel: "book", Symbol: symbols, Depth: depth, Snapshot: ptr(true)}, ReqID: ReqIDBook})
	return []wsfeed.Subscription{{Name: "trade", Msg: trade}, {Name: "book", Msg: book}}
}

type request struct {
	Method string `json:"method"`
	Params params `json:"params"`
	ReqID  int64  `json:"req_id,omitempty"`
}

type params struct {
	Channel  string   `json:"channel"`
	Symbol   []string `json:"symbol"`
	Depth    int      `json:"depth,omitempty"`
	Snapshot *bool    `json:"snapshot,omitempty"`
}

func ptr[T any](v T) *T { return &v }

// Header holds the top-level fields of a Kraken v2 message that the client
// inspects. Channel messages have Channel (and usually Type); responses to
// requests have Method, Success and Result.
type Header struct {
	Channel string `json:"channel"`
	Type    string `json:"type"`
	Method  string `json:"method"`
	Success *bool  `json:"success"`
	Error   string `json:"error"`
	Symbol  string `json:"symbol"` // top-level symbol of a rejected subscribe
	ReqID   int64  `json:"req_id"`
	// Result is filled for subscribe responses only; other responses may
	// carry a differently shaped result, which is left alone.
	Result SubscribeResult `json:"-"`
}

// SubscribeResult is the result object of a subscribe response.
type SubscribeResult struct {
	Channel string `json:"channel"`
	Symbol  string `json:"symbol"`
}

// ParseHeader decodes the top-level fields of a message. It fails only if
// data is not a valid JSON object.
func ParseHeader(data []byte) (Header, error) {
	var raw struct {
		Header
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Header{}, err
	}
	h := raw.Header
	if h.Method == "subscribe" && len(raw.Result) > 0 {
		_ = json.Unmarshal(raw.Result, &h.Result) // best effort; only used for logs and errors
	}
	return h, nil
}

// SubscribeTarget returns the channel and symbol a subscribe response is
// about. Successful acks carry them in result; rejected ones have only a
// top-level symbol, so the channel comes from req_id.
func (h Header) SubscribeTarget() (channel, symbol string) {
	channel, symbol = h.Result.Channel, h.Result.Symbol
	if symbol == "" {
		symbol = h.Symbol
	}
	if channel == "" {
		switch h.ReqID {
		case ReqIDTrade:
			channel = "trade"
		case ReqIDBook:
			channel = "book"
		}
	}
	return channel, symbol
}

// Key names the stream a message belongs to, for stats: the channel for
// channel messages, otherwise the method of a response ("subscribe", "pong").
func (h Header) Key() string {
	switch {
	case h.Channel != "":
		return h.Channel
	case h.Method != "":
		return h.Method
	}
	return ""
}
