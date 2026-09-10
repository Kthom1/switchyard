#!/usr/bin/env bash
# Exercise the real proxy error logger against an unreachable test upstream.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p work
scratch="$(mktemp -d "$PWD/work/log-test.XXXXXX")"
export SWITCHYARD_COMPOSE_PROJECT="switchyard-log-$RANDOM-$RANDOM-$$"
container="$SWITCHYARD_COMPOSE_PROJECT-proxy"
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  scripts/plane down --volumes >/dev/null 2>&1 || true
  rm -rf "$scratch"
}
trap cleanup EXIT
cp test/fixtures/plane.env "$scratch/.env.plane"
export SWITCHYARD_PLANE_ENV="$scratch/.env.plane"
sed -E 's@(reverse_proxy .* )[^ ]+:[0-9]+@\1127.0.0.1:1@' deploy/Caddyfile > "$scratch/Caddyfile"
scripts/plane run -d --no-deps --name "$container" \
  -p 127.0.0.1::80 -v "$scratch/Caddyfile:/etc/caddy/Caddyfile:ro" proxy >/dev/null
address="$(docker port "$container" 80/tcp)"
for attempt in {1..100}; do
  if curl --max-time 2 -s -o /dev/null "http://$address/"; then break; fi
  sleep 0.1
done
code="$(curl --max-time 2 -s -o /dev/null -w '%{http_code}' -H 'X-Api-Key: switchyard-redaction-sentinel' "http://$address/")"
[[ "$code" == 502 ]]
logs="$(docker logs "$container" 2>&1)"
[[ "$logs" == *'http.log.error'* ]]
[[ "$logs" != *'switchyard-redaction-sentinel'* ]]
echo 'Proxy API token redaction passed'
