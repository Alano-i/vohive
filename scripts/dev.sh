#!/bin/sh

set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
FRONTEND_PID=""
BACKEND_PID=""
AIR_VERSION=${AIR_VERSION:-v1.64.4}

cleanup() {
  trap - INT TERM EXIT
  if [ -n "$FRONTEND_PID" ]; then
    kill "$FRONTEND_PID" 2>/dev/null || true
  fi
  if [ -n "$BACKEND_PID" ]; then
    kill "$BACKEND_PID" 2>/dev/null || true
  fi
  if [ -n "$FRONTEND_PID" ]; then
    wait "$FRONTEND_PID" 2>/dev/null || true
  fi
  if [ -n "$BACKEND_PID" ]; then
    wait "$BACKEND_PID" 2>/dev/null || true
  fi
}

trap cleanup INT TERM EXIT

cd "$ROOT_DIR"

mkdir -p data logs .cache/air

if [ ! -f config/config.yaml ]; then
  mkdir -p config
  cp config/config.example.yaml config/config.yaml
  printf '%s\n' "已创建本地开发配置：config/config.yaml（默认账号 admin / admin）"
fi

if [ ! -x web/node_modules/.bin/vite ]; then
  printf '%s\n' "正在安装前端依赖..."
  npm ci --prefix web
fi

printf '%s\n' "启动 VoHive 联合开发环境"
printf '%s\n' "前端：http://127.0.0.1:5173"
printf '%s\n' "后端：http://127.0.0.1:7575"
printf '%s\n' "前端修改由 Vite 热更新，Go/YAML 修改会自动重启后端"

npm run dev --prefix web -- --host 0.0.0.0 &
FRONTEND_PID=$!

go run "github.com/air-verse/air@${AIR_VERSION}" -c .air.toml &
BACKEND_PID=$!

while kill -0 "$FRONTEND_PID" 2>/dev/null && kill -0 "$BACKEND_PID" 2>/dev/null; do
  sleep 1
done

if ! kill -0 "$FRONTEND_PID" 2>/dev/null; then
  wait "$FRONTEND_PID"
else
  wait "$BACKEND_PID"
fi
