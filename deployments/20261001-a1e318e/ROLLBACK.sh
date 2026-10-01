#!/usr/bin/env bash
set -euo pipefail
umask 077
B=/opt/new-api/backups/update-20261001-a1e318e
if [ "$#" -gt 0 ] && [ "$1" = --verify-copy ]; then
  mkdir -p "$B/rollback-copy"
  cp "$B/compose.modified.yml" "$B/rollback-copy/compose.yml"
  cp "$B/compose.before.yml" "$B/rollback-copy/compose.yml"
  cmp "$B/compose.before.yml" "$B/rollback-copy/compose.yml"
  docker compose --project-directory /opt/new-api -p new-api -f "$B/rollback-copy/compose.yml" config -q
  cmp "$B/Caddyfile.before" /opt/new-api/caddy/Caddyfile
  docker image inspect new-api-local-rollback:20261001-before-a1e318e >/dev/null
  echo ROLLBACK_COPY=PASS_CONFIG_HASH_RESTORED_OLD_IMAGE_AVAILABLE
  # 旧镜像在无网络、无生产卷的临时容器运行，验证真实恢复启动而不是只比配置。
  copy_id=$(docker run -d --pull never --network none --env SQL_DSN=local --env SQLITE_PATH=/data/rollback.db --env NODE_NAME=rollback-verification-copy new-api-local-rollback:20261001-before-a1e318e)
  trap 'docker rm -f "$copy_id" >/dev/null 2>&1' EXIT
  ready=false
  for i in $(seq 1 60); do
    if docker exec "$copy_id" wget -q -O - http://127.0.0.1:3000/api/status > "$B/rollback-copy/status.json" 2>/dev/null; then ready=true; break; fi
    sleep 1
  done
  test "$ready" = true
  python3 - "$B/rollback-copy/status.json" <<'PY'
import json,sys
x=json.load(open(sys.argv[1])); assert x['success'] and x['data']['version']==''
print('ROLLBACK_RUNTIME=PASS_OLD_IMAGE_HTTP_200_VERSION_UNCHANGED')
PY
  docker rm -f "$copy_id" >/dev/null
  trap - EXIT
  exit 0
fi
# 后续发生新发布则拒绝覆盖；只恢复应用镜像，不倒灌数据库和用户数据。
cmp "$B/compose.modified.yml" /opt/new-api/docker-compose.yml
cmp "$B/Caddyfile.before" /opt/new-api/caddy/Caddyfile
active=$(docker exec new-api-postgres psql -X -q -v ON_ERROR_STOP=1 -U root -d new-api -Atc "SELECT count(*) FROM async_relay_tasks WHERE node_id='new-api-node-blue-20260930' AND status NOT IN ('succeeded','failed','cancelled');")
test "$active" = 0
cat "$B/compose.before.yml" > /opt/new-api/docker-compose.yml
docker compose --project-directory /opt/new-api -p new-api -f /opt/new-api/docker-compose.yml up -d --no-deps --pull never --timeout 150 new-api-blue
for i in $(seq 1 90); do
  if [ "$(docker inspect -f '{{.State.Health.Status}}' new-api-blue)" = healthy ]; then break; fi
  sleep 2
done
test "$(docker inspect -f '{{.State.Health.Status}}' new-api-blue)" = healthy
test "$(docker inspect -f '{{.Image}}' new-api-blue)" = sha256:10c40ebc7e667f7d8b1f6e1084867038e5d1891f10b4711b632ffff5cd6c46f6
curl -fsS --max-time 10 http://127.0.0.1:3002/api/status | python3 -c 'import sys,json;x=json.load(sys.stdin);assert x["success"] and x["data"]["version"]=="";print("ROLLBACK=PASS_OLD_IMAGE_HEALTHY_NODE_AND_DATA_PRESERVED")'
