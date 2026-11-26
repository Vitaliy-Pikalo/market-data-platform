# market-data-platform

Real-time crypto market-data platform on Kubernetes. Public exchange WebSocket feeds (Coinbase, Kraken, Binance.US) are ingested by Go services, written to Kafka (Strimzi), stored in ClickHouse (Altinity operator + Keeper), and served as statistics in Grafana. The focus is operational: replication, failover, backups, restore drills, chaos testing and measured end-to-end latency.

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

`cmd/recorder` records raw Coinbase Advanced Trade market data to disk (no Kafka). It subscribes without
auth to `market_trades`, `level2` and `heartbeats` for the given products, one subscribe message per channel.

```sh
make record                                   # BTC-USD,ETH-USD,SOL-USD into ./data
make record SYMBOLS=BTC-USD DATA_DIR=/mnt/raw
go run ./cmd/recorder -symbols BTC-USD,ETH-USD -out data -stats-interval 60s
```

Stop with Ctrl+C: the current file is flushed and closed.

**Output:** `data/coinbase/YYYY-MM-DD/HH.jsonl.zst` (UTC, rotated hourly), one line per WebSocket message:

```json
{"recv_ns":<unix nanos>,"src":"coinbase","raw":<original message, unmodified>}
```

- The open file is named `HH.jsonl.zst.partial` and is renamed when closed cleanly, so a leftover
  `.partial` file means the recorder crashed. A restart within the same hour writes `HH.1.jsonl.zst`
  instead of overwriting.
- Read a file with `zstd -dc data/coinbase/2026-11-26/19.jsonl.zst | head -n 5 | jq .`
- Why this format: [ADR 0002](docs/adr/0002-raw-recorder-storage-format.md).

**Reconnects:** exponential backoff with full jitter (500 ms base, 30 s cap), reset after 60 s of healthy
connection. A connection with no message for 30 s is treated as dead. Every reconnect is logged.

**Gap detection:** `sequence_num` is tracked per connection (reset on reconnect); skipped numbers are logged
with expected, got and missing count.

**Logs:** JSON (`log/slog`) on stderr, including a cumulative `stats` line every 60 s with msgs, bytes, dropped non-JSON messages,
reconnects and gaps per channel.

## Repo layout

```
cmd/recorder/      raw WebSocket recorder binary (pre-Kafka data capture)
internal/          shared packages: sink, backoff, seqcheck, feed/coinbase
ingestor/          per-exchange ingestors -> Kafka
clickhouse/        DDL, materialized views
deploy/kind/       kind cluster config
deploy/helm/       Helm values for Strimzi, ClickHouse, Grafana, etc.
docs/postmortems/  one postmortem per incident (see TEMPLATE.md)
docs/runbooks/     operational runbooks
scripts/           ops/automation scripts
```
