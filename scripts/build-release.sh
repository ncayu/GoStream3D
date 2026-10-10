#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

(
  cd web
  npm ci
  npm run build
)

mkdir -p internal/api/static/assets dist
find internal/api/static/assets -maxdepth 1 -type f -name 'index-*' -delete
cp web/dist/index.html internal/api/static/index.html
cp web/dist/assets/* internal/api/static/assets/
go build -trimpath -o dist/gostream3d ./cmd/server

echo "Built dist/gostream3d with embedded web UI"
