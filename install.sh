#!/bin/sh
# Installs lossmon-client from a GitHub release (Linux amd64, macOS amd64/arm64).
# Env: LOSSMON_VERSION (default: latest release), LOSSMON_INSTALL_DIR (default: ~/.local/bin),
# LOSSMON_BASE_URL (https:// or file:// directory holding the archive and SHA256SUMS).
set -eu

die() {
  echo "install.sh: $*" >&2
  exit 1
}

main() {
  repo=https://github.com/logos-messaging/logos-delivery-loss-monitor
  install_dir=${LOSSMON_INSTALL_DIR:-$HOME/.local/bin}
  version=${LOSSMON_VERSION:-}

  case "$(uname -s)-$(uname -m)" in
    Linux-x86_64) platform=linux_amd64 ;;
    Darwin-x86_64)
      platform=darwin_amd64
      [ "$(sysctl -n sysctl.proc_translated 2>/dev/null)" = 1 ] && platform=darwin_arm64
      ;;
    Darwin-arm64) platform=darwin_arm64 ;;
    *) die "unsupported platform $(uname -s) $(uname -m); supported: linux amd64, darwin amd64, darwin arm64" ;;
  esac

  command -v curl >/dev/null 2>&1 || die "curl is required"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256() { sha256sum -c "$1"; }
  elif command -v shasum >/dev/null 2>&1; then
    sha256() { shasum -a 256 -c "$1"; }
  else
    die "sha256sum or shasum is required"
  fi

  if [ -n "${LOSSMON_BASE_URL:-}" ]; then
    case "$LOSSMON_BASE_URL" in
      https://* | file://*) ;;
      *) die "LOSSMON_BASE_URL must start with https:// or file://" ;;
    esac
    echo "Using LOSSMON_BASE_URL=$LOSSMON_BASE_URL" >&2
    protos='=https,file'
  else
    protos='=https'
  fi

  if [ -z "$version" ]; then
    location=$(curl --proto '=https' --tlsv1.2 -fsS -o /dev/null -w '%{redirect_url}' "$repo/releases/latest") ||
      die "cannot resolve the latest release"
    version=${location##*/v}
    [ -n "$version" ] && [ "$version" != "$location" ] || die "cannot resolve the latest release"
  fi
  version=${version#v}

  base=${LOSSMON_BASE_URL:-$repo/releases/download/v$version}
  archive=lossmon-client_${version}_${platform}.tar.gz

  if [ -d "$install_dir/lossmon-client" ]; then
    die "$install_dir/lossmon-client is a directory; remove it or set LOSSMON_INSTALL_DIR"
  fi

  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  trap 'exit 1' INT TERM HUP

  echo "Downloading lossmon-client $version ($platform)"
  curl --proto "$protos" --tlsv1.2 -fsSL -o "$tmp/$archive" "$base/$archive" || die "cannot download $base/$archive"
  curl --proto "$protos" --tlsv1.2 -fsSL -o "$tmp/SHA256SUMS" "$base/SHA256SUMS" || die "cannot download $base/SHA256SUMS"

  grep " $archive\$" "$tmp/SHA256SUMS" > "$tmp/check" || die "$archive is not listed in SHA256SUMS"
  (cd "$tmp" && sha256 check >/dev/null) || die "checksum mismatch for $archive"

  tar -xzf "$tmp/$archive" -C "$tmp"
  mkdir -p "$install_dir"
  install -m 755 "$tmp/${archive%.tar.gz}/lossmon-client" "$install_dir/.lossmon-client.$$"
  mv -f "$install_dir/.lossmon-client.$$" "$install_dir/lossmon-client"

  echo "Installed $install_dir/lossmon-client"
  case ":$PATH:" in
    *":$install_dir:"*) ;;
    *) echo "Add $install_dir to your PATH to run lossmon-client" ;;
  esac
}

main "$@"
