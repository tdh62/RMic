#!/usr/bin/env bash
set -euo pipefail
set +x
cd "$(dirname "$0")/.."
: "${ANDROID_HOME:?Set ANDROID_HOME to the installed Android SDK}"
case "${1:-all}" in
  all) variants=(debug release) ;;
  debug|release) variants=("$1") ;;
  *) printf 'Usage: package-android.sh [all|debug|release]\n' >&2; exit 1 ;;
esac
commit=$(git rev-parse HEAD)
version=${RMIC_VERSION:-dev-${commit:0:8}}
if [[ ${CNB_EVENT:-} == tag_push ]]; then version=${RMIC_VERSION:-$CNB_BRANCH}; fi
if [[ ! $version =~ ^[A-Za-z0-9][A-Za-z0-9._+-]{0,79}$ ]]; then
  printf 'Invalid Android package version\n' >&2
  exit 1
fi
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT
# PR builds produce an unsigned Release without loading a release signing identity.
if [[ ${CNB_EVENT:-} == pull_request ]]; then
  unset RMIC_ANDROID_KEYSTORE RMIC_ANDROID_KEYSTORE_BASE64 RMIC_ANDROID_STORE_PASSWORD RMIC_ANDROID_KEY_ALIAS RMIC_ANDROID_KEY_PASSWORD
fi
if [[ -n ${RMIC_ANDROID_KEYSTORE_BASE64:-} ]]; then
  if [[ -n ${RMIC_ANDROID_KEYSTORE:-} ]]; then
    printf 'Provide either a keystore path or its base64 content\n' >&2
    exit 1
  fi
  printf '%s' "$RMIC_ANDROID_KEYSTORE_BASE64" | base64 --decode > "$temporary/signing.keystore"
  chmod 600 "$temporary/signing.keystore"
  export RMIC_ANDROID_KEYSTORE="$temporary/signing.keystore"
  unset RMIC_ANDROID_KEYSTORE_BASE64
fi
release_signing=unsigned
if [[ -n ${RMIC_ANDROID_KEYSTORE:-}${RMIC_ANDROID_STORE_PASSWORD:-}${RMIC_ANDROID_KEY_ALIAS:-}${RMIC_ANDROID_KEY_PASSWORD:-} ]]; then
  : "${RMIC_ANDROID_KEYSTORE:?Missing keystore path or base64 content}"
  : "${RMIC_ANDROID_STORE_PASSWORD:?Missing store password}"
  : "${RMIC_ANDROID_KEY_ALIAS:?Missing key alias}"
  : "${RMIC_ANDROID_KEY_PASSWORD:?Missing key password}"
  [[ -f $RMIC_ANDROID_KEYSTORE ]] || { printf 'Keystore file does not exist\n' >&2; exit 1; }
  export RMIC_ANDROID_KEYSTORE
  release_signing=signed
fi
if [[ ${RMIC_ANDROID_REQUIRE_SIGNING:-0} == 1 && $release_signing != signed ]]; then
  printf 'Android Release signing is required but no signing identity was provided\n' >&2
  exit 1
fi
tasks=(:app:testDebugUnitTest :app:lintDebug :app:assembleDebugAndroidTest)
for variant in "${variants[@]}"; do
  if [[ $variant == release ]]; then tasks+=(:app:assembleRelease :app:lintRelease)
  else tasks+=(:app:assembleDebug); fi
done
(cd android && ./gradlew --no-daemon -PrmicVersion="$version" "${tasks[@]}")
mkdir -p dist
for variant in "${variants[@]}"; do
  name="rmic-$version-android-$variant"
  signing=debug
  source="android/app/build/outputs/apk/$variant/app-$variant.apk"
  if [[ $variant == release ]]; then
    signing=$release_signing
    if [[ $signing == unsigned ]]; then
      name+=-unsigned
      source=android/app/build/outputs/apk/release/app-release-unsigned.apk
    fi
  fi
  cp "$source" "dist/$name.apk"
  badging=$("$ANDROID_HOME/build-tools/35.0.0/aapt" dump badging "dist/$name.apk")
  if [[ $variant == release && $badging == *application-debuggable* ]]; then
    printf 'Release APK must not be debuggable\n' >&2
    exit 1
  fi
  if [[ $signing == unsigned ]]; then
    if "$ANDROID_HOME/build-tools/35.0.0/apksigner" verify "dist/$name.apk" >/dev/null 2>&1; then
      printf 'Unexpected signature on an unsigned Release\n' >&2
      exit 1
    fi
  else
    "$ANDROID_HOME/build-tools/35.0.0/apksigner" verify "dist/$name.apk"
  fi
  mkdir -p "$temporary/$name"
  cp "dist/$name.apk" "$temporary/$name/"
  cp README.md "$temporary/$name/README.md"
  cp android/THIRD-PARTY-NOTICES.md "$temporary/$name/THIRD-PARTY-NOTICES.md"
  printf 'Version: %s\nCommit: %s\nVariant: %s\nSigning: %s\nBuilt: %s\n' \
    "$version" "$commit" "$variant" "$signing" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$temporary/$name/BUILDINFO.txt"
  (cd "$temporary" && zip -qr "$name.zip" "$name")
  cp "$temporary/$name.zip" dist/
  (cd dist && sha256sum "$name.apk" > "$name.apk.sha256" && sha256sum "$name.zip" > "$name.zip.sha256")
  printf 'Built dist/%s.apk and dist/%s.zip (signing: %s)\n' "$name" "$name" "$signing"
done
