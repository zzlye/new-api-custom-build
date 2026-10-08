#!/usr/bin/env bash
set -euo pipefail
# 仅恢复指定的验收副本，不连接服务器、不变更正在运行的新容器。
base="$(cd "$(dirname "$0")" && pwd)"
target="${1:?请指定验收副本}"
test "$target" = "$base/rollback-copy.json"
cp -- "$base/ORIGINAL_FILE.json" "$target"
echo "ROLLBACK: receipt_original_restored; production_unchanged"
