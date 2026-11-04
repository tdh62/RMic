#!/usr/bin/env bash
set -euo pipefail

# Run directly in CNB's standard Go environment; no image build is needed.
apt-get update
apt-get install -y --no-install-recommends \
  libopus-dev pkg-config pulseaudio pulseaudio-utils zip unzip file
if [[ ${1:-linux} != windows ]]; then exit 0; fi

apt-get install -y --no-install-recommends \
  gcc-mingw-w64-x86-64-posix dpkg-dev autoconf automake libtool
# Use the distro's authenticated source packages for the Windows codec build.
sed -i 's/^Types: deb$/Types: deb deb-src/' /etc/apt/sources.list.d/debian.sources
apt-get update
build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT
cd "$build_dir"
apt-get source opus
cd opus-*/
# Debian's native hardening flags must not leak into a MinGW target.
./configure --host=x86_64-w64-mingw32 --prefix=/opt/opus-windows \
  --disable-shared --enable-static --disable-doc --disable-extra-programs \
  CC=x86_64-w64-mingw32-gcc CFLAGS='-O2 -U_FORTIFY_SOURCE -D_FORTIFY_SOURCE=0' CPPFLAGS=''
make -j4
make install
cp COPYING /opt/opus-windows/COPYING
