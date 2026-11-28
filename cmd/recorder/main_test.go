package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name                     string
		exchange, symbols, url   string
		wantSymbols              []string
		wantURL, wantDir, wantEr string
	}{
		{
			name: "coinbase defaults", exchange: "coinbase",
			wantSymbols: []string{"BTC-USD", "ETH-USD", "SOL-USD"},
			wantURL:     "wss://advanced-trade-ws.coinbase.com", wantDir: filepath.Join("data", "coinbase"),
		},
		{
			name: "kraken defaults", exchange: "kraken",
			wantSymbols: []string{"BTC/USD", "ETH/USD", "SOL/USD"},
			wantURL:     "wss://ws.kraken.com/v2", wantDir: filepath.Join("data", "kraken"),
		},
		{
			name: "explicit symbols and url", exchange: "kraken", symbols: " BTC/USD , ,XRP/USD", url: "ws://localhost:1",
			wantSymbols: []string{"BTC/USD", "XRP/USD"}, wantURL: "ws://localhost:1", wantDir: filepath.Join("data", "kraken"),
		},
		{name: "unknown exchange", exchange: "binance", wantEr: `unknown -exchange "binance" (want coinbase|kraken)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := resolve(tt.exchange, tt.symbols, tt.url, "data")
			if tt.wantEr != "" {
				if err == nil || err.Error() != tt.wantEr {
					t.Fatalf("err = %v, want %q", err, tt.wantEr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if o.ex.name != tt.exchange || !reflect.DeepEqual(o.symbols, tt.wantSymbols) || o.url != tt.wantURL || o.dir != tt.wantDir {
				t.Errorf("got exchange=%s symbols=%v url=%s dir=%s, want %s %v %s %s",
					o.ex.name, o.symbols, o.url, o.dir, tt.exchange, tt.wantSymbols, tt.wantURL, tt.wantDir)
			}
		})
	}
}

func TestAppendRecord(t *testing.T) {
	for _, src := range []string{"coinbase", "kraken"} {
		raw := `{"channel":"heartbeat"}`
		line := appendRecord(nil, 1795722796662522500, src, []byte(raw))
		want := `{"recv_ns":1795722796662522500,"src":"` + src + `","raw":{"channel":"heartbeat"}}`
		if string(line) != want {
			t.Errorf("appendRecord = %s, want %s", line, want)
		}
		if !json.Valid(line) || strings.Contains(string(line), "\n") {
			t.Errorf("record is not one valid JSON line: %s", line)
		}
	}
}
