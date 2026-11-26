// Command recorder records raw Coinbase Advanced Trade market data to disk.
//
// Each WebSocket message becomes one line
//
//	{"recv_ns":<unix nanos>,"src":"coinbase","raw":<original json>}
//
// in <out>/coinbase/YYYY-MM-DD/HH.jsonl.zst (UTC, rotated hourly).
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
	"github.com/Vitaliy-Pikalo/market-data-platform/internal/seqcheck"
	"github.com/Vitaliy-Pikalo/market-data-platform/internal/sink"
)

const src = "coinbase"

func main() {
	symbols := flag.String("symbols", "BTC-USD,ETH-USD,SOL-USD", "comma-separated product IDs")
	out := flag.String("out", "data", "output root; files go to <out>/coinbase/YYYY-MM-DD/HH.jsonl.zst")
	url := flag.String("url", coinbase.DefaultURL, "Coinbase Advanced Trade WebSocket URL")
	statsEvery := flag.Duration("stats-interval", 60*time.Second, "interval between stats log lines")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(log)

	if err := run(log, splitList(*symbols), filepath.Join(*out, src), *url, *statsEvery); err != nil {
		log.Error("recorder failed", "err", err.Error())
		os.Exit(1)
	}
}

func run(log *slog.Logger, products []string, dir, url string, statsEvery time.Duration) error {
	if len(products) == 0 {
		return fmt.Errorf("no symbols given")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	w := sink.New(dir)
	st := newStats()
	client := coinbase.New(coinbase.Config{URL: url, Products: products, Logger: log})

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

	log.Info("recorder starting", "products", products, "dir", dir, "url", url)
	var line []byte
	runErr := client.Run(ctx, coinbase.Handler{
		Message: func(m coinbase.Message) error {
			raw := m.Raw
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
			line = appendRecord(line[:0], m.RecvNs, raw)
			if err := w.Write(line); err != nil {
				return err
			}
			st.message(m.Channel, len(m.Raw))
			return nil
		},
		Gap: func(channel string, r seqcheck.Result) {
			st.gap(channel, r)
		},
		Reconnect: func(int, time.Duration, error) {
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

// appendRecord appends {"recv_ns":N,"src":"coinbase","raw":RAW} to dst.
func appendRecord(dst []byte, recvNs int64, raw []byte) []byte {
	dst = append(dst, `{"recv_ns":`...)
	dst = strconv.AppendInt(dst, recvNs, 10)
	dst = append(dst, `,"src":"`+src+`","raw":`...)
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
		slog.Group("channels", attrs...))
}
