#!/usr/bin/env bash
set -euo pipefail
# GUI-only dependencies: the ordinary TUI setup never requires a display stack.
if [[ ${1:-linux} == windows ]]; then
  # MinGW and static Opus are supplied by the existing Windows setup stage.
  exit 0
fi
apt-get update
apt-get install -y --no-install-recommends libgl1-mesa-dev xorg-dev xvfb xauth fonts-noto-cjk
