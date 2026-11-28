// Command recorder records raw exchange market data to disk, one exchange
// per process (see docs/adr/0003-one-recorder-process-per-exchange.md).
//
// Each WebSocket message becomes one line
//
//	{"recv_ns":<unix nanos>,"src":"<exchange>","raw":<original json>}
//
// in <out>/<exchange>/YYYY-MM-DD/HH.jsonl.zst (UTC, rotated hourly).
// Ctrl+C flushes and closes the current file.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Vitaliy-Pikalo/market-data-platform/internal/feed/coinbase"
	"github.com/Vitaliy-Pikalo/market-data-platform/internal/feed/kraken"
	"github.com/Vitaliy-Pikalo/market-data-platform/internal/seqcheck"
	"github.com/Vitaliy-Pikalo/market-data-platform/internal/sink"
)

// exchange describes one supported feed.
type exchange struct {
	name           string
	defaultURL     string
	defaultSymbols []string
	// run streams until ctx is done or a fatal error, reporting through h.
	run func(ctx context.Context, log *slog.Logger, url string, symbols []string, h hooks) error
}

// hooks are the feed-independent callbacks the recorder wires into a feed.
type hooks struct {
	message   func(recvNs int64, channel string, raw []byte) error
	gap       func(channel string, r seqcheck.Result)
	dropped   func(raw []byte, err error)
	reconnect func(attempt int, wait time.Duration, cause error)
}

var exchanges = map[string]exchange{
	"coinbase": {
		name:           "coinbase",
		defaultURL:     coinbase.DefaultURL,
		defaultSymbols: []string{"BTC-USD", "ETH-USD", "SOL-USD"},
		run: func(ctx context.Context, log *slog.Logger, url string, symbols []string, h hooks) error {
			return coinbase.New(coinbase.Config{URL: url, Products: symbols, Logger: log}).Run(ctx, coinbase.Handler{
				Message:   func(m coinbase.Message) error { return h.message(m.RecvNs, m.Channel, m.Raw) },
				Gap:       h.gap,
				Dropped:   h.dropped,
				Reconnect: h.reconnect,
			})
		},
	},
	"kraken": {
		name:           "kraken",
		defaultURL:     kraken.DefaultURL,
		defaultSymbols: kraken.DefaultSymbols,
		run: func(ctx context.Context, log *slog.Logger, url string, symbols []string, h hooks) error {
			// Kraken messages carry no sequence number, so there is no gap hook.
			return kraken.New(kraken.Config{URL: url, Symbols: symbols, Logger: log}).Run(ctx, kraken.Handler{
				Message:   func(m kraken.Message) error { return h.message(m.RecvNs, m.Channel, m.Raw) },
				Dropped:   h.dropped,
				Reconnect: h.reconnect,
			})
		},
	},
}

func exchangeNames() string {
	names := make([]string, 0, len(exchanges))
	for n := range exchanges {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, "|")
}

// options are the resolved command-line settings.
type options struct {
	ex      exchange
	symbols []string
	url     string
	dir     string
}

// resolve applies per-exchange defaults to the raw flag values.
func resolve(exName, symbols, url, out string) (options, error) {
	ex, ok := exchanges[exName]
	if !ok {
		return options{}, fmt.Errorf("unknown -exchange %q (want %s)", exName, exchangeNames())
	}
	o := options{ex: ex, symbols: splitList(symbols), url: url, dir: filepath.Join(out, ex.name)}
	if len(o.symbols) == 0 {
		o.symbols = ex.defaultSymbols
	}
	if o.url == "" {
		o.url = ex.defaultURL
	}
	return o, nil
}

func main() {
	exName := flag.String("exchange", "coinbase", "exchange to record: "+exchangeNames())
	symbols := flag.String("symbols", "", "comma-separated symbols (default per exchange: coinbase BTC-USD,ETH-USD,SOL-USD; kraken BTC/USD,ETH/USD,SOL/USD)")
	out := flag.String("out", "data", "output root; files go to <out>/<exchange>/YYYY-MM-DD/HH.jsonl.zst")
	url := flag.String("url", "", "WebSocket URL (default per exchange)")
	statsEvery := flag.Duration("stats-interval", 60*time.Second, "interval between stats log lines")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(log)

	o, err := resolve(*exName, *symbols, *url, *out)
	if err == nil {
		err = run(log, o, *statsEvery)
	}
	if err != nil {
		log.Error("recorder failed", "exchange", *exName, "err", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger, o options, statsEvery time.Duration) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	w := sink.New(o.dir)
	st := newStats()

	statsDone := make(chan struct{})
	go func() {
		defer close(statsDone)
		t := time.NewTicker(statsEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				st.log(log, "stats")
			}
		}
	}()

	log.Info("recorder starting", "exchange", o.ex.name, "symbols", o.symbols, "dir", o.dir, "url", o.url)
	var line []byte
	runErr := o.ex.run(ctx, log, o.url, o.symbols, hooks{
		message: func(recvNs int64, channel string, raw []byte) error {
			n := len(raw)
			if bytes.IndexByte(raw, '\n') >= 0 {
				// Newlines can only be insignificant whitespace in valid JSON;
				// strip them so the record stays on one line.
				var buf bytes.Buffer
				if err := json.Compact(&buf, raw); err != nil {
					return fmt.Errorf("compact message: %w", err)
				}
				raw = buf.Bytes()
				st.compacted()
			}
			line = appendRecord(line[:0], recvNs, o.ex.name, raw)
			if err := w.Write(line); err != nil {
				return err
			}
			st.message(channel, n)
			return nil
		},
		gap: func(channel string, r seqcheck.Result) {
			st.gap(channel, r)
		},
		dropped: func(raw []byte, _ error) {
			st.dropped(len(raw))
		},
		reconnect: func(int, time.Duration, error) {
			st.reconnect()
		},
	})
	stop()
	<-statsDone

	path := w.CurrentPath()
	closeErr := w.Close()
	st.log(log, "final stats")
	if closeErr != nil {
		log.Error("closing output file failed", "path", path, "err", closeErr.Error())
	} else if path != "" {
		log.Info("output file closed", "path", path)
	}
	if runErr != nil {
		return runErr
	}
	return closeErr
}

// appendRecord appends {"recv_ns":N,"src":SRC,"raw":RAW} to dst. src must
// not need JSON escaping (it is one of the exchange names above).
func appendRecord(dst []byte, recvNs int64, src string, raw []byte) []byte {
	dst = append(dst, `{"recv_ns":`...)
	dst = strconv.AppendInt(dst, recvNs, 10)
	dst = append(dst, `,"src":"`...)
	dst = append(dst, src...)
	dst = append(dst, `","raw":`...)
	dst = append(dst, raw...)
	return append(dst, '}')
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// stats are cumulative since start. Methods are safe for concurrent use.
type stats struct {
	mu         sync.Mutex
	start      time.Time
	channels   map[string]*channelStats
	reconnects int
	compact    int
	// Non-JSON messages are dropped before their channel is known, so they
	// are counted globally rather than per channel.
	droppedMsgs  int64
	droppedBytes int64
}

type channelStats struct {
	Msgs    int64 `json:"msgs"`
	Bytes   int64 `json:"bytes"`
	Gaps    int64 `json:"gaps"`
	Missing int64 `json:"missing"`
	Behind  int64 `json:"behind"`
}

func newStats() *stats {
	return &stats{start: time.Now(), channels: map[string]*channelStats{}}
}

func (s *stats) ch(name string) *channelStats {
	if name == "" {
		name = "(none)"
	}
	c := s.channels[name]
	if c == nil {
		c = &channelStats{}
		s.channels[name] = c
	}
	return c
}

func (s *stats) message(channel string, n int) {
	s.mu.Lock()
	c := s.ch(channel)
	c.Msgs++
	c.Bytes += int64(n)
	s.mu.Unlock()
}

func (s *stats) gap(channel string, r seqcheck.Result) {
	s.mu.Lock()
	c := s.ch(channel)
	switch r.Kind {
	case seqcheck.Gap:
		c.Gaps++
		c.Missing += r.Missing
	case seqcheck.Behind:
		c.Behind++
	}
	s.mu.Unlock()
}

func (s *stats) reconnect() {
	s.mu.Lock()
	s.reconnects++
	s.mu.Unlock()
}

func (s *stats) dropped(n int) {
	s.mu.Lock()
	s.droppedMsgs++
	s.droppedBytes += int64(n)
	s.mu.Unlock()
}

func (s *stats) compacted() {
	s.mu.Lock()
	s.compact++
	s.mu.Unlock()
}

func (s *stats) log(log *slog.Logger, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.channels))
	for n := range s.channels {
		names = append(names, n)
	}
	sort.Strings(names)
	var total channelStats
	attrs := make([]any, 0, len(names))
	for _, n := range names {
		c := *s.channels[n]
		total.Msgs += c.Msgs
		total.Bytes += c.Bytes
		total.Gaps += c.Gaps
		total.Missing += c.Missing
		total.Behind += c.Behind
		attrs = append(attrs, slog.Any(n, c))
	}
	log.Info(msg,
		"uptime", time.Since(s.start).Round(time.Second).String(),
		"msgs", total.Msgs, "bytes", total.Bytes, "reconnects", s.reconnects,
		"gaps", total.Gaps, "missing", total.Missing, "behind", total.Behind,
		"compacted", s.compact,
		"dropped", s.droppedMsgs, "dropped_bytes", s.droppedBytes,
		slog.Group("channels", attrs...))
}
