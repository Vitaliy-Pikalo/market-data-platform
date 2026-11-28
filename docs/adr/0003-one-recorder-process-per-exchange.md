# 0003. One recorder process per exchange

- **Status:** Accepted
- **Date:** 2026-11-28
- **Issue/PR:** #3

## Context

The recorder (ADR 0002) started with Coinbase only. Kraken is the second exchange and Binance.US is
planned. Each exchange has its own WebSocket protocol, subscription format, heartbeat behaviour and
failure modes. Coinbase has a per-connection `sequence_num`; Kraken has none, but has book checksums
and rejects bad subscriptions with `success:false`.

We need to decide whether one recorder process records all exchanges, or one process records one exchange.

In both designs the shared connection logic (dial, read limit, idle timeout, backoff, reconnect) lives in
`internal/wsfeed`, and each exchange is a small package on top of it (`internal/feed/coinbase`,
`internal/feed/kraken`). The question is only about how many feeds one process runs.

The target platform is Kubernetes (Phase 0 onward), where restarts, resource limits, health and logs are
per container.

## Decision

We will run **one recorder process per exchange**, selected with `-exchange coinbase|kraken`. Each process
writes only to `data/<exchange>/` and sets `src` to the exchange name. Running several exchanges means
running several processes (later: one Deployment, or one container, per exchange).

## Alternatives

### One process, many feeds

- Pros: one binary invocation; one place for shared config and stats; slightly less memory.
- Cons:
  - Failure isolation: a crash, panic, memory blow-up or sink error in one feed stops all exchanges.
    A fatal configuration error on one exchange (such as a rejected Kraken symbol, which deliberately
    stops the recorder) would stop the others, or would need extra logic to stop only that feed.
  - Restarts and deploys: changing one exchange's symbols restarts all captures, creating gaps everywhere.
  - Operations: logs, stats, liveness and resource limits are mixed. Kubernetes restarts and limits work
    per container, not per goroutine.
  - More code: per-feed lifecycle, error routing and shutdown handling inside one process.

### One process per exchange *and* per symbol

- Pros: maximum isolation.
- Cons: many connections per exchange (Kraken allows about 150 connection attempts per IP per 10
  minutes), many processes and files for little benefit. Symbols of one exchange share a connection and
  fail together anyway.

## Consequences

- An exchange outage, a bug or a bad config affects only that exchange's data and process.
- A rejected subscription can exit the process (exit 1) without touching other exchanges; the supervisor
  (shell, later Kubernetes) shows it as a failed container.
- Each process opens its own hourly files under `data/<exchange>/`, so processes never write to the same file.
- Starting all exchanges locally needs several commands (or a small script); in Kubernetes it is one
  Deployment per exchange.
- Cross-exchange views (for example comparing timestamps) happen downstream, in Kafka or ClickHouse,
  not in the recorder.
- Revisit if the number of exchanges grows enough that per-process overhead matters, or if a feed needs
  data from another exchange at record time.
