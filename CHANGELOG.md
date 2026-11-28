# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and releases are tagged per phase as `v0.N.0-phaseN` (see [CONTRIBUTING.md](CONTRIBUTING.md#releases-and-tags)).

## [Unreleased]

### Added

- Repository conventions: `CLAUDE.md`, `CONTRIBUTING.md`, pull request and issue templates, ADR template and ADR 0001, this changelog.
- Coinbase raw WebSocket recorder (`cmd/recorder`, `make record`): records `market_trades`, `level2` and `heartbeats`
  to hourly `data/coinbase/YYYY-MM-DD/HH.jsonl.zst` files, with reconnect backoff, sequence gap detection and
  periodic stats ([#2](https://github.com/Vitaliy-Pikalo/market-data-platform/issues/2), [ADR 0002](docs/adr/0002-raw-recorder-storage-format.md)).
- Kraken raw WebSocket v2 recorder (`-exchange kraken`, `make record EXCHANGE=kraken`): records `trade` and `book`
  (depth 10) to `data/kraken/YYYY-MM-DD/HH.jsonl.zst`; a rejected subscription stops the recorder
  ([#3](https://github.com/Vitaliy-Pikalo/market-data-platform/issues/3), [ADR 0003](docs/adr/0003-one-recorder-process-per-exchange.md)).

### Changed

- The recorder runs one exchange per process, selected with `-exchange` (default `coinbase`); output goes to
  `data/<exchange>/` and `src` is the exchange name. `-symbols` and `-url` default per exchange.
- Shared WebSocket session handling (dial, read limit, idle timeout, backoff, reconnect) moved to `internal/wsfeed`.

[Unreleased]: https://github.com/Vitaliy-Pikalo/market-data-platform/commits/main
