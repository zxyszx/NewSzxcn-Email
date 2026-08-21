#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA_DIR="$ROOT_DIR/apps/api/data-local-preview"

mkdir -p "$DATA_DIR"

(
  cd "$ROOT_DIR/apps/api"
  LANQIN_DATA_DIR="$DATA_DIR" \
  LANQIN_DB_PATH="$DATA_DIR/lanqin.db" \
  LANQIN_ADDR=127.0.0.1:18082 \
  LANQIN_ADMIN_EMAIL=admin@lanqin.local \
  LANQIN_ADMIN_PASSWORD=admin \
  LANQIN_PUBLIC_BASE_URL=http://127.0.0.1:5173 \
  LANQIN_ALLOW_INSECURE_HTTP=true \
  go run ./cmd/server
) &
API_PID=$!

cleanup() {
  kill "$API_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "本地预览账号：admin@lanqin.local / admin"
echo "本地预览地址：http://127.0.0.1:5173/login"
VITE_API_TARGET=http://127.0.0.1:18082 pnpm --dir "$ROOT_DIR/apps/web" dev --host 127.0.0.1
