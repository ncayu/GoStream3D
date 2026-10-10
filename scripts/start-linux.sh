#!/usr/bin/env bash
set -euo pipefail
directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$directory"
exec "$directory/gostream3d" "$@"
