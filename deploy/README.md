# Deploying OpenReserve + ORPay

This runs a single-producer network and the ORPay wallet on one Linux server,
with HTTPS on your own domain. It suits a closed pilot. It is not a
decentralized network: whoever holds the proposer key controls block
production.

## 1. Prepare

- A Linux server with Docker and the Compose plugin, ports 80 and 443 open.
- A DNS `A` (and optionally `AAAA`) record for your domain pointing at it.
- This repository cloned on the server, e.g. at `/opt/openreserve`.

## 2. Create keys and genesis (on a trusted machine, not the server)

```bash
make build
bin/orctl keygen -key proposer.json
bin/orctl words  -key proposer.json     # write these 24 words down and store them offline
bin/orctl keygen -key treasury.json     # holds the initial supply
bin/orctl words  -key treasury.json
bin/orctl genesis -chain-id openreserve-pilot-1 \
  -proposer $(bin/orctl address -key proposer.json) \
  -alloc $(bin/orctl address -key treasury.json)=1000000 > genesis.json
```

To issue naira, add the NGN asset with its issuer and transaction fee (₦20 here):

```bash
bin/orctl keygen -key issuer.json
# add to the genesis command above:
#   -asset "NGN:$(bin/orctl address -key issuer.json):20:2:Nigerian naira"
```

Naira fees go to the issuer (your platform revenue); ORP fees are burned.
Fees are fixed in genesis: changing them later means a new genesis (or a
governance upgrade, not built yet).

Copy `genesis.json` to `deploy/genesis.json` on the server.

## 3. Configure secrets

```bash
cp deploy/.env.example deploy/.env && chmod 600 deploy/.env
```

Set `DOMAIN` and `ORP_PROPOSER_SEED` (`bin/orctl export-seed -key proposer.json`).
Set `ORPAY_FAUCET_SEED` only on test networks, using a separate key with a
small balance. Never put the treasury key on the server.

Better than a file: inject `ORP_PROPOSER_SEED` from your platform's secrets
manager (Docker/Swarm secrets, AWS Secrets Manager, Vault, 1Password Connect)
so it never touches the server's disk. The node reads it from the
environment and refuses key files that other users can read.

## 4. Start

```bash
cd deploy
docker compose up -d --build
curl https://$DOMAIN/v1/status
```

Open `https://$DOMAIN` for the wallet. Caddy obtains the TLS certificate
automatically.

## 5. Back up

```bash
crontab -e
# 0 * * * * cd /opt/openreserve/deploy && ./backup.sh
```

Each backup holds the block log, the username directory and genesis.
Copy `deploy/backups/` off the server too (object storage, another host).

**Restore:** stop the stack, create a fresh `node-data` volume, copy the
unzipped `blocks.jsonl` into it, then start again. The node replays and
re-verifies every block on startup.

## Monitoring

```bash
crontab -e
# * * * * * cd /opt/openreserve/deploy && ./monitor.sh
```

`monitor.sh` checks the node's `/healthz` (producer loop alive, replica in
sync, enough disk), the ORPay backend, and that the naira reserve fully backs
the NGN in circulation. It posts to `ALERT_WEBHOOK` when something breaks and
again when it recovers.

Inside the Docker network the node also serves Prometheus metrics at
`http://node:8080/metrics` (height, block age, mempool, supply, issued assets,
pools, open and disputed escrows). It is not exposed publicly.

## Rate limits

Per client IP: 600 reads/min and 60 transactions/min at the node; signups
10/min and faucet 5/min at the ORPay backend (plus one faucet payout per
address per hour). Adjust with `-rate-read` and `-rate-submit` on the node.
