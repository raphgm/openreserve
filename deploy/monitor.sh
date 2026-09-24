#!/bin/sh
# Checks the public endpoints and alerts when something is wrong, and again
# when it recovers. Run from cron every minute:
#   * * * * * cd /opt/openreserve/deploy && ./monitor.sh
# Set ALERT_WEBHOOK in deploy/.env to a Slack/Discord/Google Chat incoming
# webhook URL (anything that accepts {"text": "..."}).
set -u
cd "$(dirname "$0")"
[ -f .env ] && . ./.env
BASE="https://${DOMAIN:?set DOMAIN in deploy/.env}"
STATE=.monitor-state
problems=""

check() { # name url jq-free-test
  body=$(curl -fsS --max-time 10 "$2" 2>&1) || { problems="$problems\n- $1 unreachable ($body)"; return; }
  case "$body" in *"$3"*) ;; *) problems="$problems\n- $1: $body" ;; esac
}

check "node" "$BASE/healthz" '"ok":true'
check "ORPay backend" "$BASE/api/config" '"faucet"'
if [ -n "${PAYSTACK_SECRET_KEY:-}${FLW_SECRET_KEY:-}" ]; then
  check "gateway reserve" "$BASE/pay/reserve" '"fully_backed":true'
fi

prev=$(cat "$STATE" 2>/dev/null || echo ok)
if [ -n "$problems" ]; then
  now="fail"
  msg="OpenReserve ($DOMAIN) needs attention:$problems"
else
  now="ok"
  msg="OpenReserve ($DOMAIN) recovered: all checks passing."
fi
if [ "$now" != "$prev" ] && [ -n "${ALERT_WEBHOOK:-}" ]; then
  text=$(printf '%b' "$msg" | sed 's/\\/\\\\/g; s/"/\\"/g' | awk '{printf "%s\\n", $0}')
  curl -fsS --max-time 10 -H 'Content-Type: application/json' -d "{\"text\":\"$text\"}" "$ALERT_WEBHOOK" >/dev/null || true
fi
echo "$now" > "$STATE"
[ "$now" = ok ] || printf '%b\n' "$msg" >&2
