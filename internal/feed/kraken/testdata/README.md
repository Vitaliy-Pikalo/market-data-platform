# Kraken WebSocket v2 fixtures

`doc_*.json` are the JSON examples from the Kraken WebSocket v2 docs, copied unchanged
(fetched 2026-11-28 from `https://docs.kraken.com/exchange/api-reference/spot-websocket-v2/<page>.md`):

| File | Page | Example |
|---|---|---|
| `doc_trade_subscribe_request.json` | `trade.md` | Subscribe Request |
| `doc_trade_subscribe_response.json` | `trade.md` | Subscribe Response |
| `doc_trade_snapshot.json` | `trade.md` | Snapshot |
| `doc_trade_update.json` | `trade.md` | Update |
| `doc_book_subscribe_request.json` | `book.md` | Subscribe Request |
| `doc_book_subscribe_response_1.json`, `_2.json` | `book.md` | Subscribe Response (the page shows one ack per symbol in one block) |
| `doc_book_snapshot.json` | `book.md` | Snapshot |
| `doc_book_update.json` | `book.md` | Update |
| `doc_heartbeat.json` | `heartbeat.md` | Example: Heartbeat |
| `doc_status_update.json` | `status.md` | Example: Update |
| `doc_pong.json` | `ping.md` | Response |

`live_subscribe_rejected.json` is **not** from the docs, which show no failure example. It was received
from `wss://ws.kraken.com/v2` on 2026-11-28 after subscribing to `trade` for `FOO/BAR`. It has no
`result` object; `symbol` and `req_id` are at the top level.
