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

## Repo layout

```
recorder/          raw WebSocket recorder (pre-Kafka data capture)
ingestor/          per-exchange ingestors -> Kafka
clickhouse/        DDL, materialized views
deploy/kind/       kind cluster config
deploy/helm/       Helm values for Strimzi, ClickHouse, Grafana, etc.
docs/postmortems/  one postmortem per incident (see TEMPLATE.md)
docs/runbooks/     operational runbooks
scripts/           ops/automation scripts
```
