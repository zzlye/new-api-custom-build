#!/usr/bin/env bash
set -euo pipefail
# 本脚本只回退本次入口及配置，保留更新期间的数据和在途任务。
B=/opt/new-api/backups/update-20260930-0da2aa9
if [ "${1:-}" = --verify-copy ]; then
  mkdir -p "$B/rollback-copy"
  cp "$B/compose.modified.yml" "$B/rollback-copy/compose.yml"
  cp "$B/Caddyfile.modified" "$B/rollback-copy/Caddyfile"
  cp "$B/compose.before.yml" "$B/rollback-copy/compose.yml"
  cp "$B/Caddyfile.before" "$B/rollback-copy/Caddyfile"
  cmp "$B/compose.before.yml" "$B/rollback-copy/compose.yml"
  cmp "$B/Caddyfile.before" "$B/rollback-copy/Caddyfile"
  docker compose --project-directory /opt/new-api -p new-api -f "$B/rollback-copy/compose.yml" config -q
  docker run --rm --network none -v "$B/rollback-copy/Caddyfile:/tmp/Caddyfile:ro" caddy:2-alpine caddy adapt --config /tmp/Caddyfile --adapter caddyfile --validate >/dev/null 2>&1
  echo ROLLBACK_COPY=PASS
  exit 0
fi
# 后续部署发生变动时拒绝覆盖，防止误用历史回退。
cmp "$B/Caddyfile.modified" /opt/new-api/caddy/Caddyfile
cmp "$B/compose.modified.yml" /opt/new-api/docker-compose.yml
docker start new-api-green-20260922 >/dev/null
for i in $(seq 1 30); do
  if curl -fsS --max-time 2 http://127.0.0.1:3001/api/status | grep -q '"success":true'; then break; fi
  sleep 1
done
curl -fsS --max-time 10 http://127.0.0.1:3001/api/status | grep -q '"success":true'
# 原位写入避免单文件挂载仍指向旧 inode。
cat "$B/Caddyfile.before" > /opt/new-api/caddy/Caddyfile
docker exec new-api-caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
cat "$B/compose.before.yml" > /opt/new-api/docker-compose.yml
echo ROLLBACK=PASS_OLD_ROUTING_RESTORED_NEW_INSTANCE_RETAINED
