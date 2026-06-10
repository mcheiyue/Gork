#!/usr/bin/env sh
set -eu

/app/scripts/init_storage.sh

export HOST="${HOST:-${SERVER_HOST:-0.0.0.0}}"
export PORT="${PORT:-${SERVER_PORT:-8000}}"

if [ "$#" -eq 0 ]; then
  set -- /app/grok2api
fi

exec "$@"
