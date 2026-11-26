.PHONY: record up down test

SYMBOLS ?= BTC-USD,ETH-USD,SOL-USD
DATA_DIR ?= data

# Record raw Coinbase market data to $(DATA_DIR)/coinbase/YYYY-MM-DD/HH.jsonl.zst until Ctrl+C.
record:
	go run ./cmd/recorder -symbols $(SYMBOLS) -out $(DATA_DIR)

up:
	@echo "TODO: create kind cluster + deploy stack"

down:
	@echo "TODO: tear down"

test:
	go test ./...
