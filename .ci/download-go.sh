#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
module_dir=${1:-.}
case "$module_dir" in .|desktop) ;; *) printf 'Unsupported Go module: %s\n' "$module_dir" >&2; exit 1 ;; esac
# A pipe falls back on any proxy error (including EOF/5xx), unlike a comma
# which falls back only on 404/410. Keep normal go.sum and sumdb verification.
export GOPROXY=${GOPROXY:-https://goproxy.cn|direct}
for attempt in 1 2 3; do
  printf 'Downloading Go dependencies for %s (attempt %s/3, limit 180s)\n' "$module_dir" "$attempt"
  if timeout 180s go -C "$module_dir" mod download; then
    go -C "$module_dir" mod verify
    exit 0
  else
    status=$?
  fi
  if [[ $attempt == 3 ]]; then
    printf 'Dependency download failed after 3 attempts (last exit: %s)\n' "$status" >&2
    exit "$status"
  fi
  sleep "$((attempt * 2))"
done
