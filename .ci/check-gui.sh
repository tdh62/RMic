#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
unformatted=$(gofmt -l desktop/cmd desktop/ui desktop/internal)
if [[ -n "$unformatted" ]]; then printf 'GUI Go files need formatting:\n%s\n' "$unformatted" >&2; exit 1; fi
bash .ci/download-go.sh desktop
go -C desktop vet ./...
go -C desktop test -race -tags ci ./...
# Compilation above and software-renderer tests do not establish hardware,
# input-method, accessibility, or driver compatibility on a desktop machine.
