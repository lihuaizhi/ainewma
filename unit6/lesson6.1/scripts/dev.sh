#!/usr/bin/env bash
# dev.sh 一键拉起双进程（ride-ops :8081 + gateway :8080）。
#   ./scripts/dev.sh            # offline 引擎（无 key 可跑）
#   DEEPSEEK_API_KEY=sk-... ./scripts/dev.sh   # 自动切 LLM 主链路
# Ctrl-C 优雅退出两个进程。
set -euo pipefail
cd "$(dirname "$0")/.."

#mkdir -p data bin
echo "[dev] building…"
#go build -o bin/ride-mcp ./cmd/ride-mcp
#go build -o bin/server ./cmd/server

echo "[dev] starting ride-mcp :8081 …"
./bin/ride-mcp &
RIDE_PID=$!
trap 'kill $RIDE_PID 2>/dev/null || true' EXIT
sleep 0.5

echo "[dev] starting gateway :8080 …"
./bin/server &
GW_PID=$!
trap 'kill $RIDE_PID $GW_PID 2>/dev/null || true' EXIT

echo "[dev] ready → http://127.0.0.1:8080  (CLI: go run ./cmd/cli)"
wait
