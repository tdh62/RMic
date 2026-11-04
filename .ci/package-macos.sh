#!/usr/bin/env bash
# Build on a Mac with Xcode Command Line Tools and native Homebrew dependencies.
set -euo pipefail
cd "$(dirname "$0")/.."
arch=${1:-$(go env GOHOSTARCH)}
if [[ $(uname -s) != Darwin ]]; then
  printf 'macOS packages require a Mac with Xcode Command Line Tools; Linux cross compilation is not configured.\n' >&2
  exit 1
fi
case "$arch" in amd64|arm64) ;; *) printf 'Unsupported macOS architecture: %s\n' "$arch" >&2; exit 1 ;; esac
if [[ $arch != $(go env GOHOSTARCH) ]]; then
  printf 'Build with a native %s Go toolchain and matching Homebrew Opus installation.\n' "$arch" >&2
  exit 1
fi
for tool in clang pkg-config brew otool; do command -v "$tool" >/dev/null; done
commit=$(git rev-parse HEAD)
version=${RMIC_VERSION:-dev-${commit:0:8}}
if [[ ! $version =~ ^[A-Za-z0-9][A-Za-z0-9._+-]{0,79}$ ]]; then printf 'Invalid package version\n' >&2; exit 1; fi
build_date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
name="rmic-$version-darwin-$arch"
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT
package="$staging/$name"
mkdir -p "$package/THIRD-PARTY-NOTICES" "$staging/pkgconfig" dist
opus_prefix=$(brew --prefix opus)
[[ -f "$opus_prefix/lib/libopus.a" ]] || { printf 'Install native Opus: brew install opus pkg-config\n' >&2; exit 1; }
# Link the archive directly so the download also runs on Macs without Homebrew.
cat > "$staging/pkgconfig/opus.pc" <<EOF
Name: opus
Description: static libopus for RMic
Version: $(PKG_CONFIG_PATH="$opus_prefix/lib/pkgconfig" pkg-config --modversion opus)
Libs: $opus_prefix/lib/libopus.a -lm
Cflags: -I$opus_prefix/include/opus
EOF
export CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" CC=clang
export PKG_CONFIG_LIBDIR="$staging/pkgconfig" PKG_CONFIG_PATH="$staging/pkgconfig"
export MACOSX_DEPLOYMENT_TARGET=12.0
ldflags="-s -w -X cnb.cool/tdh6/RMic/internal/buildinfo.Version=$version -X cnb.cool/tdh6/RMic/internal/buildinfo.Commit=$commit -X cnb.cool/tdh6/RMic/internal/buildinfo.Date=$build_date"
go test ./...
go vet ./...
go build -trimpath -buildvcs=false -ldflags="$ldflags" -o "$package/rmic" ./cmd/rmic
otool -L "$package/rmic" > "$staging/dependencies.txt"
if grep -E '(/opt/homebrew/|/usr/local/|libopus.*dylib)' "$staging/dependencies.txt"; then
  printf 'Unexpected external runtime dependency\n' >&2; exit 1
fi
cp .ci/macos/Start-RMic.command "$package/Start-RMic.command"
chmod +x "$package/Start-RMic.command"
cp README.md "$package/"
cp LICENSE "$package/LICENSE"
cp "$(go env GOROOT)/LICENSE" "$package/THIRD-PARTY-NOTICES/Go-LICENSE"
license=$(find -H "$opus_prefix" -maxdepth 3 -type f \( -iname COPYING -o -iname LICENSE \) -print -quit)
[[ -n $license ]] || { printf 'Cannot locate Homebrew Opus license\n' >&2; exit 1; }
cp "$license" "$package/THIRD-PARTY-NOTICES/libopus-COPYING"
go list -m -f '{{if .Dir}}{{.Path}}|{{.Dir}}{{end}}' all > "$staging/modules.txt"
while IFS='|' read -r module directory; do
  [[ -n $directory ]] || continue
  destination="$package/THIRD-PARTY-NOTICES/${module//\//_}"
  mkdir -p "$destination"
  find "$directory" -maxdepth 1 -type f \( -iname 'license*' -o -iname 'copying*' -o -iname 'notice*' \) -exec cp {} "$destination/" \;
done < "$staging/modules.txt"
printf 'Version: %s\nCommit: %s\nBuilt: %s\nTarget: darwin/%s\nMinimum macOS: 12.0\nToolchain: %s\n' "$version" "$commit" "$build_date" "$arch" "$(go version)" > "$package/BUILDINFO.txt"
"$package/rmic" --version
"$package/rmic" --help >/dev/null 2>&1
archive="$name.tar.gz"
tar -C "$staging" -czf "dist/$archive" "$name"
(cd dist && shasum -a 256 "$archive" > "$archive.sha256")
printf 'Built dist/%s (unsigned, not notarized)\n' "$archive"
