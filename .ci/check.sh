#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

unformatted=$(gofmt -l cmd internal drivers/windows web/embed.go)
if [[ -n "$unformatted" ]]; then
  printf 'Go files need formatting:\n%s\n' "$unformatted" >&2
  exit 1
fi
bash .ci/download-go.sh .
go vet ./...

runtime_dir=$(mktemp -d)
pulse_pid=''
cleanup() {
  if [[ -n "$pulse_pid" ]]; then
    kill "$pulse_pid" 2>/dev/null || true
    wait "$pulse_pid" 2>/dev/null || true
  fi
  rm -rf "$runtime_dir"
}
trap cleanup EXIT
export PULSE_SERVER="unix:$runtime_dir/native"
# GitHub job containers run as root with a runner-provided HOME. PulseAudio
# must use an unprivileged user and directories owned by that user instead.
mkdir -m 700 -p "$runtime_dir/home" "$runtime_dir/run"
pulse_command=(env "HOME=$runtime_dir/home" "XDG_RUNTIME_DIR=$runtime_dir/run")
if [[ $(id -u) == 0 ]]; then
  chown -R pulse:pulse "$runtime_dir"
  pulse_command=(runuser -u pulse -- "${pulse_command[@]}")
fi
"${pulse_command[@]}" pulseaudio -n --daemonize=no --use-pid-file=no --exit-idle-time=-1 \
  --log-target="file:$runtime_dir/pulse.log" \
  --load="module-native-protocol-unix socket=$runtime_dir/native auth-anonymous=1" \
  --load=module-always-sink &
pulse_pid=$!
ready=false
for ((attempt=0; attempt<100; attempt++)); do
  if pactl info >/dev/null 2>&1; then ready=true; break; fi
  if ! kill -0 "$pulse_pid" 2>/dev/null; then break; fi
  sleep 0.1
done
if [[ "$ready" != true ]]; then
  tail -n 80 "$runtime_dir/pulse.log" >&2
  exit 1
fi
# Native test packages share this PulseAudio server and fixed virtual route names.
# Serialize packages so independent suites cannot create ambiguous duplicate devices.
RMIC_NATIVE_TEST=1 go test -p 1 -race -timeout=90s ./...
pactl list short sinks | awk '$2 ~ /^rmic_/ { leaked=1 } END { exit leaked }'
pactl list short sources | awk '$2 ~ /^rmic_/ { leaked=1 } END { exit leaked }'
