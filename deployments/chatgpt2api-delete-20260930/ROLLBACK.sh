#!/usr/bin/env bash
set -euo pipefail
# 删除是永久操作；此脚本只验证目标已不存在，恢复需要重新部署并重新添加账号。
if [ "${1:-}" = "--verify-copy" ]; then
  test ! -e /mnt/data100/chatgpt2api
  test ! -e /etc/systemd/system/chatgpt2api-docker.service
  test -z "$(docker ps -aq --filter name=chatgpt2api)"
  test -z "$(docker images --format '{{.Repository}}:{{.Tag}}' | grep '^chatgpt2api-local' || true)"
  printf 'ROLLBACK_COPY=PASS DELETED_STATE_CONFIRMED=PASS\n'
  exit 0
fi
printf 'ROLLBACK=NOT_AVAILABLE_PERMANENT_DELETE_REQUIRES_REDEPLOY\n' >&2
exit 1