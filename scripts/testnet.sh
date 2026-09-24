#!/bin/sh
# Run a local multi-validator OpenReserve network (CometBFT consensus).
#   scripts/testnet.sh start [N]   generate (if needed) and start N validators (default 4)
#   scripts/testnet.sh stop        stop them
# Node i serves the API on :808i. Logs: testnet/node<i>.log
set -eu
cd "$(dirname "$0")/.."
cmd=${1:-start}
n=${2:-4}
case "$cmd" in
start)
  make build >/dev/null
  (cd node && go build -o ../bin/orp-testnet ./cmd/orp-testnet)
  [ -d testnet ] || bin/orp-testnet -n "$n" -out testnet >/dev/null
  i=0
  for home in testnet/node*/; do
    home=${home%/}
    bin/openreserved -genesis testnet/genesis.json -data "$home/orp" -cometbft-home "$home" \
      -listen ":$((8080 + i))" >"$home.log" 2>&1 &
    echo "started ${home##*/} (API :$((8080 + i)), pid $!)"
    i=$((i + 1))
  done
  echo "demo wallet: testnet/keys/alice.json (1,000,000 ORP). Try:"
  echo "  bin/orctl status"
  ;;
stop)
  pkill -f "cometbft-home testnet/node" && echo stopped || echo "not running"
  ;;
*)
  echo "usage: $0 start [N] | stop" >&2
  exit 2
  ;;
esac
