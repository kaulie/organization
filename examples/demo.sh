#!/usr/bin/env bash
# End-to-end demo of the organization API: builds, starts a throwaway server,
# exercises every requirement, then shuts the server down.
set -euo pipefail

PORT="${PORT:-18080}"
BASE="http://127.0.0.1:${PORT}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$(mktemp -d)/org-server"

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

echo "==> building server"
(cd "${ROOT}" && go build -o "${BIN}" ./cmd/server)

echo "==> starting server on ${BASE}"
PORT="${PORT}" "${BIN}" >/dev/null 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 50); do
  if curl -sf "${BASE}/healthz" >/dev/null 2>&1; then break; fi
  sleep 0.1
done

post() { curl -sf -X POST "${BASE}/$1" -H 'Content-Type: application/json' -d "$2"; echo; }
get() { curl -sf "${BASE}/$1"; echo; }

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
echo "==> 5. list endpoints"
get api/v1/persons
get 'api/v1/persons?departmentId=D0002'
get api/v1/departments

echo
echo "==> 6. error cases (expected 4xx)"
curl -s -o /dev/null -w 'duplicate department name      -> %{http_code}\n' \
  -X POST "${BASE}/api/v1/departments" -H 'Content-Type: application/json' -d '{"name":"研发中心","type":"研发"}'
curl -s -o /dev/null -w 'invalid department type        -> %{http_code}\n' \
  -X POST "${BASE}/api/v1/departments" -H 'Content-Type: application/json' -d '{"name":"财务部","type":"finance"}'
curl -s -o /dev/null -w 'unknown department on register -> %{http_code}\n' \
  -X POST "${BASE}/api/v1/persons" -H 'Content-Type: application/json' -d '{"name":"Eve","id":"user-003","type":"HUMAN","departmentId":"D9999"}'
curl -s -o /dev/null -w 'unknown department view        -> %{http_code}\n' "${BASE}/api/v1/departments/D9999"
curl -s -o /dev/null -w 'unknown person view            -> %{http_code}\n' "${BASE}/api/v1/persons/nobody"

echo
echo "==> demo finished"
