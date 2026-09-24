#!/bin/sh
# Copies the chain and username data out of the running stack into
# ./backups/<timestamp>/. Safe while running: the block log is append-only,
# and a torn final line is dropped automatically on restore.
# Run from cron, e.g.:  0 * * * * cd /opt/openreserve/deploy && ./backup.sh
set -eu
cd "$(dirname "$0")"
dest="backups/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$dest"
docker compose cp node:/data/blocks.jsonl "$dest/blocks.jsonl"
docker compose cp orpay-backend:/data/orpay-users.json "$dest/orpay-users.json" 2>/dev/null || true
cp genesis.json "$dest/"
gzip "$dest/blocks.jsonl"
# Keep the newest 72 backups.
ls -1d backups/*/ | sort -r | tail -n +73 | xargs rm -rf
echo "backup written to $dest"
