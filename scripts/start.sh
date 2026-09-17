#!/usr/bin/env bash
#
# 启动 organization —— 遵循部署系统规范的 runtime 脚本。
#
# 由控制面以 restartCmd 调用：cwd = runtimeDir，且注入
#   PORT        = 服务契约 healthUrl 里的端口（平台统一通用名；本脚本映射为
#                 SERVICE_PORT —— 服务自身只认 SERVICE_PORT/ORG_ADDR，见下方说明）
#   RUNTIME_DIR = runtimeDir
#   APP_VERSION = 本次部署的 8 位短 hash
#
# runtime 布局（backend/ 下的内容由平台在部署时保留，不会被 --delete 清掉）：
#   bin/orgd                可执行文件（来自发版包）
#   scripts/*.sh            本目录（来自发版包）
#   backend/.env            可选项覆盖（首次启动自动生成，权限 600）
#   backend/data/           数据目录（org-store.json 持久化于此，跨发布保留）
#   backend/runtime.pid     进程号
#   backend/server.log      标准输出/错误
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUNTIME_DIR="${RUNTIME_DIR:-$(cd "${DIR}/.." && pwd)}"
# 平台按服务契约注入的端口：先固化，避免后面 source backend/.env 时被覆盖。
PLATFORM_PORT="${PORT:-4250}"
APP_VERSION="${APP_VERSION:-dev}"

BIN="${RUNTIME_DIR}/bin/orgd"
BACKEND="${RUNTIME_DIR}/backend"
ENV_FILE="${BACKEND}/.env"
DATA_DIR="${BACKEND}/data"
PID_FILE="${BACKEND}/runtime.pid"
LOG_FILE="${BACKEND}/server.log"

log() { echo "[start] $*"; }
die() { echo "[start][错误] $*" >&2; exit 1; }

[ -x "${BIN}" ] || die "缺少可执行文件 ${BIN}（发版包内容不完整？）"

mkdir -p "${BACKEND}" "${DATA_DIR}"

# 首次启动生成 backend/.env：只放可选覆盖项，权限 600，绝不入 git。
# 监听地址由本脚本按契约端口推导，避免契约换端口后 .env 里的旧值把服务卡在旧端口上。
if [ ! -f "${ENV_FILE}" ]; then
  umask 077
  cat > "${ENV_FILE}" <<EOF
# organization 运行期配置（首次启动自动生成，权限 600，请勿提交到 git）
# 监听地址由 start.sh 按平台端口推导（127.0.0.1:PORT），如需换网卡再打开下面一行：
# ORG_BIND=127.0.0.1
# 数据目录（持久化 JSON 文件 org-store.json 所在目录；发布时该目录会被保留）：
# ORG_DATA_DIR=${DATA_DIR}
EOF
  chmod 600 "${ENV_FILE}"
  log "已生成 ${ENV_FILE}"
fi

# shellcheck disable=SC1090
set -a; . "${ENV_FILE}"; set +a

# 平台注入的值优先：端口永远跟随服务契约的 healthUrl。
# 服务自身读 SERVICE_PORT（刻意不用通用名 PORT：同主机其它运行时会导出 PORT，
# 例如 web-cursor 的 4211，直接继承会把本服务顶到别人的端口上），
# 这里把平台注入的 PORT 显式映射为 SERVICE_PORT。
export SERVICE_PORT="${PLATFORM_PORT}"
export ORG_ADDR="${ORG_BIND:-127.0.0.1}:${SERVICE_PORT}"
export ORG_DATA_DIR="${ORG_DATA_DIR:-${DATA_DIR}}"
export APP_VERSION

# 探活：/health 是平台对每个服务统一探的路径。
HEALTH_URL="http://127.0.0.1:${SERVICE_PORT}/health"

# 已在运行则不重复拉起（平台重启前都会先 stop，这里是防御性检查）。
if [ -f "${PID_FILE}" ]; then
  old="$(tr -d '[:space:]' < "${PID_FILE}" || true)"
  if [ -n "${old}" ] && kill -0 "${old}" 2>/dev/null; then
    log "已在运行 pid=${old}"
    exit 0
  fi
  rm -f "${PID_FILE}"
fi

log "启动 部署版本=${APP_VERSION} 监听=${ORG_ADDR} 日志=${LOG_FILE}"
nohup "${BIN}" >> "${LOG_FILE}" 2>&1 &
echo $! > "${PID_FILE}"
pid="$(cat "${PID_FILE}")"

# 探活：/health 是平台对每个服务统一探的路径。
for _ in $(seq 1 40); do
  if ! kill -0 "${pid}" 2>/dev/null; then
    rm -f "${PID_FILE}"
    echo "[start][错误] 进程已退出，最近日志：" >&2
    tail -20 "${LOG_FILE}" >&2 || true
    exit 1
  fi
  if curl -fsS -m 2 "${HEALTH_URL}" >/dev/null 2>&1; then
    log "启动成功 pid=${pid} health=${HEALTH_URL}"
    exit 0
  fi
  sleep 0.5
done

echo "[start][错误] 20s 内 ${HEALTH_URL} 未就绪，最近日志：" >&2
tail -20 "${LOG_FILE}" >&2 || true
kill "${pid}" 2>/dev/null || true
rm -f "${PID_FILE}"
exit 1
