#!/usr/bin/env bash
# One-command test network on an Oracle Cloud "Always Free" Ubuntu VM.
#
#   curl -fsSL https://raw.githubusercontent.com/raphgm/openreserve/main/deploy/oracle/setup.sh | sudo bash -s -- <ADMIN_WALLET_ADDRESS> [WEB_URL]
#
# It installs Docker, opens ports 80/443, creates the chain keys and genesis
# (test network: NGN issued with a ₦20 fee, faucet on), and starts the node,
# ORPay server, naira gateway and HTTPS proxy. The API is served at
# https://<public-ip>.sslip.io unless DOMAIN is set. WEB_URL is where the
# wallet is hosted (e.g. https://orpay.pages.dev) so shared links open there.
set -euo pipefail

ADMIN="${1:-}"
WEB_URL="${2:-}"
REPO="${REPO:-https://github.com/raphgm/openreserve.git}"
DIR=/opt/openreserve

[ "$(id -u)" = 0 ] || { echo "run with sudo"; exit 1; }

echo "==> Installing Docker and git"
apt-get update -qq
apt-get install -y -qq docker.io docker-compose-v2 git iptables-persistent >/dev/null
systemctl enable --now docker

echo "==> Opening ports 80 and 443 (Oracle images block them by default)"
for p in 80 443; do
  iptables -C INPUT -p tcp --dport $p -j ACCEPT 2>/dev/null || iptables -I INPUT 6 -p tcp --dport $p -j ACCEPT
done
iptables -C INPUT -p udp --dport 443 -j ACCEPT 2>/dev/null || iptables -I INPUT 6 -p udp --dport 443 -j ACCEPT
netfilter-persistent save >/dev/null

echo "==> Fetching OpenReserve"
if [ -d "$DIR/.git" ]; then git -C "$DIR" pull -q; else git clone -q "$REPO" "$DIR"; fi
cd "$DIR/deploy"

IP="$(curl -fsS https://api.ipify.org)"
DOMAIN="${DOMAIN:-$IP.sslip.io}"

if [ ! -f genesis.json ]; then
  echo "==> Creating keys and genesis (keys stay in $DIR/deploy/keys; back them up)"
  mkdir -p keys && chmod 700 keys
  ORCTL="docker run --rm -v $DIR/deploy/keys:/k -w /k --entrypoint orctl openreserve-node"
  docker build -q -t openreserve-node -f ../Dockerfile .. >/dev/null
  chown 10001 keys
  for k in proposer faucet issuer; do $ORCTL keygen -key /k/$k.json >/dev/null; done
  $ORCTL genesis -chain-id openreserve-testnet-1 \
    -proposer "$($ORCTL address -key /k/proposer.json)" \
    -alloc "$($ORCTL address -key /k/faucet.json)=1000000" \
    -asset "NGN:$($ORCTL address -key /k/issuer.json):20:2:Nigerian naira" > genesis.json
  umask 077
  cat > .env <<ENV
DOMAIN=$DOMAIN
WEB_URL=${WEB_URL:-https://$DOMAIN}
ORP_PROPOSER_SEED=$($ORCTL export-seed -key /k/proposer.json)
ORPAY_FAUCET_SEED=$($ORCTL export-seed -key /k/faucet.json)
ORP_ISSUER_SEED=$($ORCTL export-seed -key /k/issuer.json)
ORPAY_ADMINS=$ADMIN
# Paystack TEST key only until a licensed partner and legal sign-off are in place.
PAYSTACK_SECRET_KEY=
ENV
fi

echo "==> Starting the stack (first build takes a few minutes)"
docker compose up -d --build

echo
echo "Done. API and wallet: https://$DOMAIN"
echo "Health:  https://$DOMAIN/healthz"
echo "Add your Paystack TEST key to $DIR/deploy/.env, then: cd $DIR/deploy && docker compose up -d"
echo "Back up $DIR/deploy/keys and $DIR/deploy/.env somewhere safe and offline."
