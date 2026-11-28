# market-data-platform

[![CI](https://github.com/Vitaliy-Pikalo/market-data-platform/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Vitaliy-Pikalo/market-data-platform/actions/workflows/ci.yml)

Real-time crypto market-data platform on Kubernetes. Public exchange WebSocket feeds (Coinbase, Kraken, Binance.US) are ingested by Go services, written to Kafka (Strimzi), stored in ClickHouse (Altinity operator + Keeper), and served as statistics in Grafana. The focus is operational: replication, failover, backups, restore drills, chaos testing and measured end-to-end latency.

https://github.com/user-attachments/assets/6ff6727e-ff8d-4327-a8a7-6c586b0509a9

*55 s overview. Source: [docs/animation/](docs/animation/), re-render with `python3 render.py render`.*

## Architecture

_TODO: diagram after Phase 2._

```
exchange WS -> Go ingestor -> Kafka (RF=3) -> ClickHouse (Kafka engine + MV -> ReplicatedMergeTree) -> Grafana
```

## Phases

| # | Phase | Done when | Status |
|---|---|---|---|
| 0 | Setup: multi-node kind, Strimzi, ClickHouse, CI | `make up` builds from zero on a clean machine | ☐ |
| 1 | Ingest: per-exchange Go ingestor, normalize, reconnect/backoff, gap detection | 2 exchanges x 10 symbols for 24 h, no restarts | ☐ |
| 2 | Store: MergeTree by day, ordered by symbol+time; dedupe on trade ID | row counts match offsets; no dups after restart | ☐ |
| 3 | Stats: 1-min OHLCV, VWAP, spread, realized vol, cross-exchange gaps | dashboards live in Grafana | ☐ |
| 4 | Observability: msgs/s, consumer lag, e2e lag p50/p99 | measured at normal and 10x replay load | ☐ |
| 5 | HA: 3 brokers, RF 3, min.insync 2; 2 CH replicas; anti-affinity; PDBs | drain a node, zero data loss | ☐ |
| 6 | DR + chaos: S3-compatible backups, restore drill, Chaos Mesh, runbooks | failure -> detect -> recover -> data-lost table, each run twice | ☐ |

## Results

Numbers are listed here only after they have been reproduced twice on Linux, with the command and commit used.

_None yet._

## Recorder

`cmd/recorder` records raw exchange WebSocket market data to disk (no Kafka). It runs **one exchange per
process** ([ADR 0003](docs/adr/0003-one-recorder-process-per-exchange.md)), chosen with `-exchange`:

| `-exchange` | Endpoint (no auth) | Channels | Default `-symbols` |
|---|---|---|---|
| `coinbase` (default) | `wss://advanced-trade-ws.coinbase.com` | `market_trades`, `level2`, `heartbeats` (one subscribe per channel) | `BTC-USD,ETH-USD,SOL-USD` |
| `kraken` | `wss://ws.kraken.com/v2` | `trade` (with 50-trade snapshot), `book` depth 10 (one subscribe per channel; heartbeats are automatic) | `BTC/USD,ETH/USD,SOL/USD` |

```sh
make record                                   # coinbase, default symbols, into ./data
make record EXCHANGE=kraken
make record EXCHANGE=kraken SYMBOLS=BTC/USD DATA_DIR=/mnt/raw
go run ./cmd/recorder -exchange kraken -symbols BTC/USD,ETH/USD -out data -stats-interval 60s
```

To record both exchanges, run two processes (two terminals, or `&`). Stop each with Ctrl+C: the current
file is flushed and closed.

**Output:** `data/<exchange>/YYYY-MM-DD/HH.jsonl.zst` (UTC, rotated hourly), one line per WebSocket message:

```json
{"recv_ns":<unix nanos>,"src":"<exchange>","raw":<original message, unmodified>}
```

- The open file is named `HH.jsonl.zst.partial` and is renamed when closed cleanly, so a leftover
  `.partial` file means the recorder crashed. A restart within the same hour writes `HH.1.jsonl.zst`
  instead of overwriting. The encoder is flushed every 5 s, so a crashed file is readable up to the last flush.
- Read a file with `zstd -dc data/kraken/2026-11-28/03.jsonl.zst | head -n 5 | jq .`
- Why this format: [ADR 0002](docs/adr/0002-raw-recorder-storage-format.md).

**Reconnects:** exponential backoff with full jitter (500 ms base, 30 s cap), reset after 60 s of healthy
connection. A connection with no message for 30 s (Coinbase) or 10 s (Kraken, which sends a heartbeat about
every second) is treated as dead. Every reconnect is logged. Shared by both exchanges (`internal/wsfeed`).

**Rejected subscriptions (Kraken):** a subscribe ack with `success:false` (for example
`Currency pair not supported FOO/BAR`) is recorded, logged, and stops the recorder with exit 1 instead of
reconnecting, since a reconnect cannot fix a bad symbol.

**Gap detection:** Coinbase `sequence_num` is tracked per connection (reset on reconnect); skipped numbers
are logged with expected, got and missing count. Kraken messages have no sequence number; book integrity
will be checked with Kraken's CRC32 book checksum instead (#17).

**Logs:** JSON (`log/slog`) on stderr, including a cumulative `stats` line every 60 s with msgs, bytes,
dropped non-JSON messages, reconnects and gaps per channel.

## Repo layout

```
cmd/recorder/      raw WebSocket recorder binary (pre-Kafka data capture)
internal/          shared packages: sink, backoff, seqcheck, wsfeed, feed/coinbase, feed/kraken
ingestor/          per-exchange ingestors -> Kafka
clickhouse/        DDL, materialized views
deploy/kind/       kind cluster config
deploy/helm/       Helm values for Strimzi, ClickHouse, Grafana, etc.
docs/postmortems/  one postmortem per incident (see TEMPLATE.md)
docs/runbooks/     operational runbooks
scripts/           ops/automation scripts
```
