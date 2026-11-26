# 0002. Raw recorder storage format: hourly JSONL + zstd

- **Status:** Accepted
- **Date:** 2026-11-26
- **Issue/PR:** #2

## Context

Before Kafka exists, we need a recorder that captures exchange WebSocket feeds exactly as received. The
captures are used to:

- replay real traffic into the ingestor and Kafka later (including at 10x speed for Phase 4 load tests),
- write parser and normalizer tests against real messages,
- investigate gaps and exchange quirks after the fact.

Requirements:

- **Lossless and unmodified.** The original message bytes must be recoverable, so a parser bug can never
  corrupt the source data.
- **Receive timestamp** per message, taken by us, for latency and replay timing.
- **Crash-safe enough:** a crash may lose the last few buffered messages, but must not corrupt earlier
  data, and a crashed file must be recognisable.
- **Cheap to write** from one goroutine at feed rate, and simple to read from Go and from the shell.
- **Compact.** level2 traffic is large: a single BTC-USD snapshot is several MB of JSON.

Preliminary measurement (Windows, single 20 s run, 3 products, `market_trades` + `level2` + `heartbeats`):
908 messages, 8.37 MB of JSONL compressed to 0.72 MB with zstd at the default level (~11.7x).

## Decision

We will store each message as one JSON line

```json
{"recv_ns":1795722796662522500,"src":"coinbase","raw":{...original message...}}
```

in zstd-compressed files at `data/<src>/YYYY-MM-DD/HH.jsonl.zst` (UTC), one file per hour.

- `raw` is the original message bytes, embedded as-is (never re-encoded).
- Files are written as `HH.jsonl.zst.partial` and renamed on clean close (hourly rotation or shutdown).
  A leftover `.partial` file is the output of a crashed writer. It can still be decoded up to the last
  complete zstd block.
- Existing files are never overwritten: a restart within the same hour writes `HH.1.jsonl.zst`, and so on.
- Compression uses `github.com/klauspost/compress/zstd` (pure Go, no cgo).

## Alternatives

### Parquet

- Pros: columnar, very good compression and fast analytical scans; readable by ClickHouse, DuckDB, Spark.
- Cons: needs a schema. Raw exchange messages are nested and differ per channel and per exchange, so
  either we parse before storing (not "raw", and a parser bug loses data) or we store one string column
  (which loses Parquet's advantages). Row groups are buffered in memory and written in large chunks, so
  more data is lost on a crash and appending one message at a time is awkward. Harder to inspect with
  shell tools.
- Parquet remains a good *derived* format: normalized trades and book updates can be exported to Parquet
  later from ClickHouse or from these files.

### Write directly to Kafka

- Pros: no separate replay step; this is the target architecture from Phase 1.
- Cons: Kafka is not running yet (Phase 0), and the recorder's job is to work without it. A Kafka outage
  would stop the capture, unless the recorder buffers to disk anyway. Retention is time-limited, while we
  want a long-lived, portable archive for tests and replays. Files can be replayed into Kafka at any rate,
  but Kafka cannot easily be rewound to data it never had.

### Uncompressed JSONL, or gzip

- Uncompressed: simplest, but ~10x more disk for level2.
- gzip: universally available, but slower and compresses worse than zstd. The zstd CLI is easy to install
  (`zstd -dc file | jq`).

### One file per day, or a size-based rotation

- Daily files are large and a crash affects a whole day of data. Size-based names are harder to find by
  time. Hourly UTC files are easy to locate, delete and replay by time range.

## Consequences

- Data can be inspected with `zstd -dc data/coinbase/2026-11-26/19.jsonl.zst | head | jq .`.
- Consumers must parse `raw` per exchange and per channel; the recorder does no normalization.
- `recv_ns` is our wall clock, not the exchange time, and is only as good as the host's clock sync.
- A crash loses data still in the zstd encoder buffer (up to one block, ~128 KiB uncompressed). If that
  becomes a problem, add a periodic flush, which makes compression slightly worse.
- The recorder rejects messages that are not valid JSON (logged and dropped), since they cannot be embedded
  as `raw`. If an exchange sends non-JSON frames, this ADR needs revisiting.
- Revisit if storage volume or scan speed becomes a problem (then consider Parquet as a derived format),
  or when the ingestor writes to Kafka (the recorder may then become a Kafka consumer that archives topics).
