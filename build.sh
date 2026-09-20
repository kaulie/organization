#!/usr/bin/env bash
#
# organization 发版构建脚本 —— 遵循部署系统规范。
#
# 由发版工具调用：/Users/gaolei/deployment/bin/release.sh organization <ref>
#   - 从干净的源码树（git archive）运行
#   - 注入 APP_VERSION = 本次部署的 8 位短 hash
#   - 产物写入 ./outputs/，即“可直接运行快照”，由 release.sh 冻结成
#     /Users/gaolei/deployment/organization/deployment-<hash>/
#
# 平台强校验：outputs/scripts/restart.sh 必须存在。
#
# 产物布局（与 /Users/gaolei/runtime/<project>/ 一致）：
#   outputs/bin/orgd          可执行文件
#   outputs/scripts/{start,stop,restart}.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${ROOT}"

log() { echo "[build] $*"; }
die() { echo "[build][错误] $*" >&2; exit 1; }

APP_VERSION="${APP_VERSION:-dev}"
BIN_NAME="orgd"
OUT="${ROOT}/outputs"

log "APP_VERSION=${APP_VERSION}  BIN=${BIN_NAME}"

log "编译 Go 服务 ./cmd/server"
rm -rf "${OUT}"
mkdir -p "${OUT}/bin" "${OUT}/scripts"
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.version=${APP_VERSION}" \
  -o "${OUT}/bin/${BIN_NAME}" ./cmd/server
[ -x "${OUT}/bin/${BIN_NAME}" ] || die "编译失败：缺少 ${OUT}/bin/${BIN_NAME}"

log "装配 outputs/scripts"
install -m 0755 scripts/start.sh scripts/stop.sh scripts/restart.sh "${OUT}/scripts/"

log "复制运行期文档"
for f in README.md LICENSE; do
  [ -f "${f}" ] && install -m 0644 "${f}" "${OUT}/${f}"
done

# 平台与 deploy.sh 的强校验项
[ -f "${OUT}/scripts/restart.sh" ] || die "outputs/ 缺少 scripts/restart.sh"
[ -x "${OUT}/scripts/start.sh" ] || die "outputs/scripts/start.sh 不可执行"

# ---- 契约自动登记（注解是唯一真源）----
# 读 cmd/server/main.go + internal/httpapi/handler.go 里的 swag 注解 → 生成 OpenAPI
# → 上报服务中心（幂等：契约没变化不会写库、不会刷 revision）。
# 发版流程本来就跑在本机，而服务中心只绑 127.0.0.1:4240，所以这里是天然合适的触发点。
# 默认"尽力而为"：服务中心不可达只告警，不阻塞发版（REGISTER_CONTRACT_STRICT=1 可改成硬失败，
# SKIP_REGISTER_CONTRACT=1 跳过；两种情况下都可以事后用 scripts/register-contract.sh 补登记）。
if [ "${SKIP_REGISTER_CONTRACT:-0}" = "1" ]; then
  log "跳过契约登记（SKIP_REGISTER_CONTRACT=1）"
else
  log "登记服务契约到服务中心（注解 → OpenAPI）"
  if ! INSTANCES="${INSTANCES:-127.0.0.1:4244}" VERSION="${APP_VERSION}" \
       bash scripts/register-contract.sh; then
    [ "${REGISTER_CONTRACT_STRICT:-0}" = "1" ] && die "契约登记失败（REGISTER_CONTRACT_STRICT=1）"
    log "警告：契约登记失败（服务中心可能不可达）；发版继续，可事后补跑 scripts/register-contract.sh"
  fi
fi

log "完成 → ${OUT}/"
ls -1 "${OUT}/bin" "${OUT}/scripts"
