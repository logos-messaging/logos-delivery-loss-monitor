#!/bin/sh
# Installs the pinned Nimble release into <dir>, verifying its SHA-256.
# Usage: install-nimble.sh <version> <dir>
set -eu

version=${1:?nimble version}
dir=${2:?install dir}

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64 | Linux-amd64)
    asset=linux_x64 sha256=b71279b0ff2beb791da6ad6cb96f3625ea264661835b7d129e0bab3316e55e6e ;;
  Linux-aarch64 | Linux-arm64)
    asset=linux_aarch64 sha256=158b7c0e36a5eb56f0e3a0f967382cd323f50d577e43fc7528c3613e4ebfafdd ;;
  Darwin-x86_64)
    asset=macosx_x64 sha256=82cf5ebb9250b9f3e4c29014faa7ec7bc874881f37700648786487cd45bea696 ;;
  Darwin-arm64)
    asset=macosx_aarch64 sha256=85cc96ce8fba4f7c9bab1b6d3a9d3d523d470bf738c6085beddd9c2c84bd5e00 ;;
  *) echo "unsupported platform: $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

if [ "$version" != 0.26.0 ]; then
  echo "checksums in this script are for Nimble 0.26.0, not $version" >&2
  exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

curl -fsSL "https://github.com/nim-lang/nimble/releases/download/v$version/nimble-$asset.tar.gz" -o "$work/nimble.tar.gz"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$work/nimble.tar.gz" | cut -d' ' -f1)
else
  actual=$(shasum -a 256 "$work/nimble.tar.gz" | cut -d' ' -f1)
fi
if [ "$actual" != "$sha256" ]; then
  echo "nimble $version checksum mismatch: got $actual, want $sha256" >&2
  exit 1
fi

tar -xzf "$work/nimble.tar.gz" -C "$work"
mkdir -p "$dir"
install -m 755 "$work/nimble" "$dir/nimble"
