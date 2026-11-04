#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

target_os=${1:-linux}
target_arch=${2:-$(go env GOHOSTARCH)}
frontend=${3:-tui}
case "$frontend" in tui|gui) ;; *) printf 'Unsupported frontend: %s\n' "$frontend" >&2; exit 1 ;; esac
if [[ $target_os == darwin && $frontend == gui ]]; then
  printf 'macOS GUI packaging requires native validation and is not yet supported\n' >&2
  exit 1
fi
if [[ $target_os == darwin ]]; then exec bash .ci/package-macos.sh "$target_arch"; fi
case "$target_os/$target_arch" in
  linux/amd64|linux/arm64|windows/amd64) ;;
  *) printf 'Unsupported package target: %s/%s\n' "$target_os" "$target_arch" >&2; exit 1 ;;
esac
commit=$(git rev-parse HEAD)
version=${RMIC_VERSION:-dev-${commit:0:8}}
if [[ ${CNB_EVENT:-} == tag_push ]]; then version=${RMIC_VERSION:-$CNB_BRANCH}; fi
if [[ ! $version =~ ^[A-Za-z0-9][A-Za-z0-9._+-]{0,79}$ ]]; then
  printf 'Version must contain only letters, digits, dot, underscore, plus or hyphen\n' >&2
  exit 1
fi
build_date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT
binary=rmic
module_dir=.
if [[ $frontend == gui ]]; then binary=rmic-gui; module_dir=desktop; fi
name="$binary-$version-$target_os-$target_arch"
package="$staging/$name"
mkdir -p "$package/THIRD-PARTY-NOTICES" dist
extension=''
if [[ $target_os == windows ]]; then
  extension=.exe
  export CC=x86_64-w64-mingw32-gcc
  export PKG_CONFIG_LIBDIR=/opt/opus-windows/lib/pkgconfig
  export CGO_LDFLAGS=-static
fi
export CGO_ENABLED=1 GOOS="$target_os" GOARCH="$target_arch"
ldflags="-s -w -X cnb.cool/tdh6/RMic/internal/buildinfo.Version=$version -X cnb.cool/tdh6/RMic/internal/buildinfo.Commit=$commit -X cnb.cool/tdh6/RMic/internal/buildinfo.Date=$build_date"
if [[ $frontend == gui && $target_os == windows ]]; then ldflags="$ldflags -H windowsgui"; fi
bash .ci/download-go.sh "$module_dir"
printf 'Compiling %s for %s/%s (CGO_ENABLED=%s)\n' "$binary" "$target_os" "$target_arch" "$CGO_ENABLED"
compile_started=$SECONDS
go -C "$module_dir" build -v -trimpath -buildvcs=false -ldflags="$ldflags" -o "$package/$binary$extension" "./cmd/$binary"
printf 'Compilation completed in %ss\n' "$((SECONDS - compile_started))"
if [[ $target_os == windows ]]; then
  printf 'Checking Windows packages with go vet\n'
  vet_started=$SECONDS
  go -C "$module_dir" vet -v ./...
  printf 'Go vet completed in %ss\n' "$((SECONDS - vet_started))"
fi
printf 'Collecting package files and third-party notices\n'
cp README.md "$package/README.md"
cp LICENSE "$package/LICENSE"
printf 'Version: %s\nCommit: %s\nBuilt: %s\nTarget: %s/%s\nToolchain: %s\n' \
  "$version" "$commit" "$build_date" "$target_os" "$target_arch" "$(go version)" > "$package/BUILDINFO.txt"
cp "$(go env GOROOT)/LICENSE" "$package/THIRD-PARTY-NOTICES/Go-LICENSE"
go -C "$module_dir" list -m -f '{{if .Dir}}{{.Path}}|{{.Dir}}{{end}}' all > "$staging/modules.txt"
while IFS='|' read -r module directory; do
  [[ -n $directory ]] || continue
  destination="$package/THIRD-PARTY-NOTICES/${module//\//_}"
  mkdir -p "$destination"
  find "$directory" -maxdepth 1 -type f \( -iname 'license*' -o -iname 'copying*' -o -iname 'notice*' \) -exec cp {} "$destination/" \;
done < "$staging/modules.txt"

if [[ $target_os == windows ]]; then
  printf '%s  %s\n' b950e39f01af1d04ea623c8f6d8eb9b6ea5c477c637295fabf20631c85116bfb drivers/windows/VBCABLE_Driver_Pack45.zip | sha256sum -c -
  mkdir -p "$package/drivers/windows"
  cp drivers/windows/VBCABLE_Driver_Pack45.zip drivers/windows/README.md "$package/drivers/windows/"
  (cd "$package/drivers/windows" && sha256sum VBCABLE_Driver_Pack45.zip > SHA256SUMS)
  cp /opt/opus-windows/COPYING "$package/THIRD-PARTY-NOTICES/libopus-COPYING"
  if [[ $frontend == tui ]]; then
    awk '{printf "%s\r\n", $0}' .ci/windows/Start-RMic.cmd > "$package/Start-RMic.cmd"
  fi
  x86_64-w64-mingw32-objdump -p "$package/$binary.exe" > "$staging/pe.txt"
  if grep -Ei 'DLL Name:.*(libopus|libgcc|libwinpthread|libstdc\+\+)' "$staging/pe.txt"; then
    printf 'Unexpected non-system runtime DLL dependency\n' >&2
    exit 1
  fi
  archive="$name.zip"
  (cd "$staging" && zip -qr "$archive" "$name")
else
  cp /usr/share/doc/libopus-dev/copyright "$package/THIRD-PARTY-NOTICES/libopus-copyright"
  if [[ $frontend == gui ]]; then
    mkdir -p "$package/share/applications" "$package/share/icons/hicolor/scalable/apps"
    cp desktop/assets/rmic-gui.desktop "$package/share/applications/"
    cp desktop/assets/rmic-gui.svg "$package/share/icons/hicolor/scalable/apps/"
    ldd "$package/$binary" > "$package/RUNTIME-LIBRARIES.txt"
  fi
  if [[ $target_arch == $(go env GOHOSTARCH) ]]; then
    "$package/$binary" --version
    "$package/$binary" --help >/dev/null 2>&1
  fi
  archive="$name.tar.gz"
  tar -C "$staging" -czf "$staging/$archive" "$name"
fi
cp "$staging/$archive" "dist/$archive"
(cd dist && sha256sum "$archive" > "$archive.sha256")
printf 'Built dist/%s\n' "$archive"
