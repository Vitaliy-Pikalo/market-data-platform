.PHONY: record up down test

EXCHANGE ?= coinbase
SYMBOLS ?=
DATA_DIR ?= data

# Record raw market data for one exchange (coinbase|kraken) to
# $(DATA_DIR)/$(EXCHANGE)/YYYY-MM-DD/HH.jsonl.zst until Ctrl+C.
# SYMBOLS empty = the exchange's defaults.
record:
	go run ./cmd/recorder -exchange $(EXCHANGE) $(if $(SYMBOLS),-symbols $(SYMBOLS)) -out $(DATA_DIR)

up:
	@echo "TODO: create kind cluster + deploy stack"

down:
	@echo "TODO: tear down"

test:
	go test ./...
