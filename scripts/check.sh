#!/usr/bin/env bash
# 固定验收入口的薄封装：把参数原样转给仓库根的 check/（package main）。
#
#   bash scripts/check.sh              # 跑全部 10 个场景
#   bash scripts/check.sh -list        # 列出全部场景
#   bash scripts/check.sh --only cost
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if command -v go >/dev/null 2>&1; then
  exec go run ./check "$@"
fi
echo "需要 Go 1.24（找不到 go）" >&2
exit 2
