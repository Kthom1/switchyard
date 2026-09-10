#!/usr/bin/env bash
# Exercise the real release in a clean Linux image with no build toolchain.
set -euo pipefail
cd "$(dirname "$0")/.."
binary=$(realpath "${1:?Usage: test/runner_smoke.sh path/to/bin/symphony}")
docker run --rm --network none --read-only --tmpfs /tmp:exec -e HOME=/tmp -e PLANE_API_KEY=unused \
  -e SYMPHONY_INSTALL_DIR=/tmp/symphony -v "$binary:/symphony:ro" ubuntu:24.04 bash -euc '
for tool in mise elixir erl; do
  if command -v "$tool"; then echo "Unexpected toolchain: $tool" >&2; exit 1; fi
done
if /symphony > /tmp/ack.log 2>&1; then exit 1; fi
grep -q "This Symphony implementation is a low key engineering preview" /tmp/ack.log
cat > /tmp/WORKFLOW.md <<WORKFLOW
---
tracker:
  kind: plane
  provider:
    endpoint: http://127.0.0.1:1
    workspace: smoke
    project_identifier: SMOKE
    project_id: 00000000-0000-0000-0000-000000000001
    api_key: \$PLANE_API_KEY
workspace:
  root: /tmp/tasks
server:
  host: 127.0.0.1
  port: 8091
---
No tasks are dispatched by this smoke check.
WORKFLOW
/symphony /tmp/WORKFLOW.md --logs-root /tmp/log --i-understand-that-this-will-be-running-without-the-usual-guardrails > /tmp/runner.log 2>&1 &
pid=$!
trap "kill -TERM $pid 2>/dev/null || true" EXIT
ready=false
for attempt in {1..60}; do
  if (exec 3<>/dev/tcp/127.0.0.1/8091; printf "GET /api/v1/state HTTP/1.0\r\nHost: localhost\r\n\r\n" >&3; cat <&3) > /tmp/state 2>/dev/null; then
    if grep -q "200 OK" /tmp/state && grep -q "running" /tmp/state; then ready=true; break; fi
  fi
  kill -0 "$pid" || { cat /tmp/runner.log; exit 1; }
  sleep 1
done
$ready || { cat /tmp/runner.log; exit 1; }
kill -TERM "$pid"
wait "$pid"
test -s /tmp/WORKFLOW.md
echo "Clean Linux: acknowledgement, Plane adapter startup, dashboard API and graceful stop passed."
'
