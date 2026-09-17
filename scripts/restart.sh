#!/usr/bin/env bash
#
# 重启 organization（控制面默认 restartCmd / deploy.sh 调用的入口）。
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bash "${DIR}/stop.sh"
exec bash "${DIR}/start.sh"
