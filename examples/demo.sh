#!/usr/bin/env bash
# End-to-end demo of the organization API: builds, starts a throwaway server,
# exercises every requirement, then shuts the server down.
set -euo pipefail

# Dedicated demo port. Deliberately does *not* inherit an ambient SERVICE_PORT:
# on shared hosts that variable is often already claimed by another service
# (this machine exports SERVICE_PORT=4211), which would make the demo bind an
# occupied port and fail before its first request.
DEMO_PORT="${DEMO_PORT:-18080}"
BASE="http://127.0.0.1:${DEMO_PORT}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Binary and data file both live in a temp dir, so a demo run never touches the
# repository working tree nor the deployed data directory.
DEMO_DIR="$(mktemp -d)"
BIN="${DEMO_DIR}/org-server"
export ORG_DATA_DIR="${DEMO_DIR}/data"
DATA_FILE="${ORG_DATA_DIR}/org-store.json"

stop_server() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    # SIGTERM mirrors scripts/stop.sh — the same signal a redeploy sends.
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  SERVER_PID=""
}

cleanup() {
  stop_server
  rm -rf "${DEMO_DIR}"
}
trap cleanup EXIT

start_server() {
  SERVICE_PORT="${DEMO_PORT}" "${BIN}" >/dev/null 2>&1 &
  SERVER_PID=$!
  for _ in $(seq 1 50); do
    if curl -sf "${BASE}/health" >/dev/null 2>&1; then return 0; fi
    sleep 0.1
  done
  echo "[demo][错误] 服务在 ${BASE} 上 5s 内未就绪" >&2
  return 1
}

echo "==> building server"
(cd "${ROOT}" && go build -o "${BIN}" ./cmd/server)

echo "==> starting server on ${BASE} (data=${DATA_FILE})"
start_server

post() { curl -sf -X POST "${BASE}/$1" -H 'Content-Type: application/json' -d "$2"; echo; }
get() { curl -sf "${BASE}/$1"; echo; }
patch() { curl -sf -X PATCH "${BASE}/$1" -H 'Content-Type: application/json' -d "$2"; echo; }

echo
echo "==> 1. create departments (研发/测试/产品/管理)"
post api/v1/departments '{"name":"研发中心","type":"研发"}'
post api/v1/departments '{"name":"测试中心","type":"测试"}'
post api/v1/departments '{"name":"产品部","type":"产品"}'
post api/v1/departments '{"name":"管理部","type":"管理"}'

echo
echo "==> 2. register persons (HUMAN / AGENT) -> employee number assigned"
post api/v1/persons '{"name":"Alice","id":"user-001","type":"HUMAN","departmentId":"D0001"}'
post api/v1/persons '{"name":"Cline","id":"agent-001","type":"AGENT","departmentId":"D0001"}'
post api/v1/persons '{"name":"Bob","id":"user-002","type":"HUMAN","departmentId":"D0002"}'

echo
echo "==> 3. department detail with member list"
get api/v1/departments/D0001

echo
echo "==> 4. person detail (by person id and by employee number)"
get api/v1/persons/user-001
get api/v1/persons/E0001

echo
echo "==> 5. rename department (id / type / members are preserved)"
patch api/v1/departments/D0001 '{"name":"平台研发部"}'
get api/v1/departments/D0001
curl -s -o /dev/null -w 'rename to an existing name     -> %{http_code}\n' \
  -X PATCH "${BASE}/api/v1/departments/D0002" -H 'Content-Type: application/json' -d '{"name":"平台研发部"}'

echo
echo "==> 6. list endpoints"
get api/v1/persons
get 'api/v1/persons?departmentId=D0002'
get api/v1/departments

echo
echo "==> 7. restart persistence (a redeploy restarts the process)"
stop_server
echo "    stopped; starting a fresh process against the same data directory"
start_server
get api/v1/departments
get 'api/v1/persons?departmentId=D0001'
echo "    data file: ${DATA_FILE}"

echo
echo "==> 8. error cases (expected 4xx)"
curl -s -o /dev/null -w 'duplicate department name      -> %{http_code}\n' \
  -X POST "${BASE}/api/v1/departments" -H 'Content-Type: application/json' -d '{"name":"平台研发部","type":"研发"}'
curl -s -o /dev/null -w 'invalid department type        -> %{http_code}\n' \
  -X POST "${BASE}/api/v1/departments" -H 'Content-Type: application/json' -d '{"name":"财务部","type":"finance"}'
curl -s -o /dev/null -w 'unknown department on register -> %{http_code}\n' \
  -X POST "${BASE}/api/v1/persons" -H 'Content-Type: application/json' -d '{"name":"Eve","id":"user-003","type":"HUMAN","departmentId":"D9999"}'
curl -s -o /dev/null -w 'unknown department view        -> %{http_code}\n' "${BASE}/api/v1/departments/D9999"
curl -s -o /dev/null -w 'unknown person view            -> %{http_code}\n' "${BASE}/api/v1/persons/nobody"

echo
echo "==> demo finished"
