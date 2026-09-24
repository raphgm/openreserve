.PHONY: build test lint devnet-init devnet-up run clean

BIN := bin

build:
	cd node && go build -o ../$(BIN)/openreserved ./cmd/openreserved
	cd node && go build -o ../$(BIN)/orctl ./cmd/orctl

test:
	cd node && go test -race ./...

lint:
	cd node && test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './_experimental/*'))" && go vet ./...

# Creates devnet/ with a proposer key, a funded "alice" key and genesis.json.
devnet-init: build
	@test ! -e devnet || (echo "devnet/ exists; remove it to start over" && exit 1)
	mkdir -p devnet
	$(BIN)/orctl keygen -key devnet/proposer.json > /dev/null
	$(BIN)/orctl keygen -key devnet/alice.json > /dev/null
	$(BIN)/orctl genesis -chain-id openreserve-devnet-1 \
		-proposer $$($(BIN)/orctl address -key devnet/proposer.json) \
		-alloc $$($(BIN)/orctl address -key devnet/alice.json)=1000000 > devnet/genesis.json
	@# Readable by the non-root container user. Devnet keys only; never do this with real keys.
	chmod 644 devnet/*.json
	@echo "devnet ready. alice = $$($(BIN)/orctl address -key devnet/alice.json)"

devnet-up:
	docker compose up --build

run: build
	$(BIN)/openreserved -key devnet/proposer.json -genesis devnet/genesis.json -data devnet/data

clean:
	rm -rf $(BIN)
