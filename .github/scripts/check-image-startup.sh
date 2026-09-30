#!/usr/bin/env bash
set -euo pipefail

image="${1:?请传入待验证的镜像标签}"
log_dir="${2:-$(mktemp -d)}"
mkdir -p "$log_dir"
container=""

cleanup() {
  local status=$?
  trap - EXIT
  # 只处理本次创建的容器，不挂载或清理任何现有数据库和生产卷。
  if [[ -n "$container" ]]; then
    docker logs "$container" >"$log_dir/container.log" 2>&1 || true
    if (( status != 0 )); then
      cat "$log_dir/container.log"
    fi
    docker rm -f "$container" >/dev/null 2>&1 || true
  fi
  exit "$status"
}
trap cleanup EXIT

# 在镜像自身的临时文件系统完成首次迁移，并只绑定本机随机端口。
container=$(docker run --detach --publish 127.0.0.1::3000 \
  --env GIN_MODE=release --env SQL_DSN=local \
  --env SQLITE_PATH=/data/new-api.db "$image")
address=$(docker port "$container" 3000/tcp)
base_url="http://$address"
ready=false
deadline=$((SECONDS + 120))

# 等待监听就绪，而不是重试生成请求；启动崩溃立即失败。
while (( SECONDS < deadline )); do
  if [[ "$(docker inspect --format '{{.State.Running}}' "$container")" != true ]]; then
    echo 'STARTUP_FAILED: 容器在就绪前退出'
    exit 1
  fi
  if curl --silent --show-error --fail --max-time 3 \
    "$base_url/api/status" -o "$log_dir/status.json" 2>"$log_dir/readiness.log"; then
    if jq -e '.success == true' "$log_dir/status.json" >/dev/null; then
      ready=true
      break
    fi
  fi
  sleep 1
done
if [[ "$ready" != true ]]; then
  echo 'STARTUP_FAILED: 状态接口未在期限内就绪'
  exit 1
fi

check_http() {
  local method=$1 path=$2 expected=$3 actual
  actual=$(curl --silent --show-error --max-time 10 --request "$method" \
    --output "$log_dir/last-response.txt" --write-out '%{http_code}' "$base_url$path")
  printf '%s %s => %s (expected %s)\n' "$method" "$path" "$actual" "$expected" | tee -a "$log_dir/http-checks.log"
  if [[ "$actual" != "$expected" ]]; then
    cat "$log_dir/last-response.txt"
    return 1
  fi
}

check_http GET /api/status 200
check_http GET / 200
cp "$log_dir/last-response.txt" "$log_dir/index.html"
grep -Eiq '<!doctype html|<html' "$log_dir/index.html"

# 无令牌请求必须由原有认证层拒绝，不能落到首页或因为路由冲突退出。
check_http POST /v1/images/generations 401
check_http POST /v1/images/edits 401
check_http POST /v1/videos 401
check_http GET /v1/videos/smoke-task 401
check_http POST /v1/tasks/smoke-plugin 401
check_http GET /v1/tasks/async_smoke 401
check_http GET /v1/tasks/task_smoke 401
check_http GET /v1/tasks/async_smoke/media/0 401
check_http GET /v1/tasks/task_smoke/artifacts 401
check_http GET /v1/tasks/task_smoke/artifacts/video/content 401
check_http GET /api/status 200
test "$(docker inspect --format '{{.State.Running}}' "$container")" = true
echo 'IMAGE_STARTUP_OK' | tee -a "$log_dir/http-checks.log"
