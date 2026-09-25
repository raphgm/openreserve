.PHONY: build test lint devnet-init devnet-up run orpay-backend orpay gateway testnet testnet-stop clean

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
	$(BIN)/orctl keygen -key devnet/issuer.json > /dev/null
	$(BIN)/orctl genesis -chain-id openreserve-devnet-1 \
		-proposer $$($(BIN)/orctl address -key devnet/proposer.json) \
		-alloc $$($(BIN)/orctl address -key devnet/alice.json)=1000000 \
		-asset "NGN:$$($(BIN)/orctl address -key devnet/issuer.json):20:2:Nigerian naira" > devnet/genesis.json
	@# Seeds for the Docker devnet, passed as env vars instead of readable key files.
	umask 077 && printf 'ORP_PROPOSER_SEED=%s\nORPAY_FAUCET_SEED=%s\n' \
		$$($(BIN)/orctl export-seed -key devnet/proposer.json) \
		$$($(BIN)/orctl export-seed -key devnet/alice.json) > devnet/.env
	@echo "devnet ready. alice = $$($(BIN)/orctl address -key devnet/alice.json)"

devnet-up:
	docker compose --env-file devnet/.env up --build

run: build
	@chmod 600 devnet/proposer.json devnet/alice.json
	$(BIN)/openreserved -key devnet/proposer.json -genesis devnet/genesis.json -data devnet/data

# ORPay backend (usernames + devnet faucet funded by alice). Needs `make run`.
orpay-backend:
	@chmod 600 devnet/alice.json
	cd apps/orpay/backend && { [ ! -f ../../../.env ] || { set -a; . ../../../.env; set +a; }; } && go run . -db ../../../devnet/orpay-users.json -faucet-key ../../../devnet/alice.json \
		$$(grep -q '"assets"' ../../../devnet/genesis.json && echo -default-currency NGN -card-payments) \
		$${ORPAY_ADMINS:+-admins $$ORPAY_ADMINS} -allow-private-webhooks

# Naira gateway. Provider keys live in .env at the repo root (chmod 600,
# never committed). Copy .env.example to .env and fill it in.
# with FLW_SECRET_HASH=<your dashboard secret hash>. Test keys move no real money.
gateway:
	@test -f .env || (echo "copy .env.example to .env and add your provider keys" && exit 1)
	@test -f devnet/issuer.json || (echo "this devnet has no NGN issuer; run: rm -rf devnet && make devnet-init" && exit 1)
	@chmod 600 .env devnet/issuer.json
	cd apps/gateway && set -a && . ../../.env && set +a && \
		ORP_ISSUER_SEED=$$(../../$(BIN)/orctl export-seed -key ../../devnet/issuer.json) \
		go run . -data ../../devnet/gateway-data

# ORPay web app on http://localhost:5173 (proxies to the node and backend).
orpay:
	cd apps/orpay/frontend && npm install && npm run dev

# Multi-validator network (CometBFT): 4 validators on this machine, APIs :8080-:8083.
testnet:
	scripts/testnet.sh start 4

testnet-stop:
	scripts/testnet.sh stop

clean:
	rm -rf $(BIN)
