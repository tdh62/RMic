#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
: "${ANDROID_HOME:=/opt/android-sdk}"
export ANDROID_HOME
if [[ $(uname -m) != x86_64 ]]; then
  printf 'Android SDK setup requires an amd64 Linux build node\n' >&2
  exit 1
fi
if ! command -v javac >/dev/null || ! command -v unzip >/dev/null || ! command -v curl >/dev/null || ! command -v zip >/dev/null; then
  apt-get update
  java_package=openjdk-17-jdk-headless
  if ! apt-cache show "$java_package" >/dev/null 2>&1; then java_package=openjdk-21-jdk-headless; fi
  apt-get install -y --no-install-recommends "$java_package" ca-certificates curl unzip zip
fi
sdkmanager="$ANDROID_HOME/cmdline-tools/latest/bin/sdkmanager"
if [[ ! -x $sdkmanager ]]; then
  temporary=$(mktemp -d)
  trap 'rm -rf "$temporary"' EXIT
  curl --fail --location --retry 3 --output "$temporary/tools.zip" \
    https://dl.google.com/android/repository/commandlinetools-linux-13114758_latest.zip
  printf '%s  %s\n' 7ec965280a073311c339e571cd5de778b9975026cfcbe79f2b1cdcb1e15317ee \
    "$temporary/tools.zip" | sha256sum -c -
  unzip -q "$temporary/tools.zip" -d "$temporary"
  mkdir -p "$ANDROID_HOME/cmdline-tools"
  mv "$temporary/cmdline-tools" "$ANDROID_HOME/cmdline-tools/latest"
fi
printf 'y\n%.0s' {1..100} | "$sdkmanager" --sdk_root="$ANDROID_HOME" --licenses >/dev/null
"$sdkmanager" --sdk_root="$ANDROID_HOME" 'platforms;android-35' 'build-tools;35.0.0' 'platform-tools'
