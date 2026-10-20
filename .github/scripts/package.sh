#!/bin/sh
# Packs raw binaries into release archives and writes SHA256SUMS.
# Usage: package.sh <version> <binaries-dir> <out-dir>
# Binaries are expected at <binaries-dir>/<component>-<os>-<arch>/<binary>.
set -eu

version=${1:?version without leading v}
bins=${2:?binaries dir}
out=${3:?output dir}

mkdir -p "$out"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for dir in "$bins"/*/; do
  target=$(basename "$dir")
  component=${target%%-*}
  platform=${target#*-}
  os=${platform%-*}
  arch=${platform#*-}
  case "$component" in
    lossmon_server) name=lossmon-server; doc=deploy/README.md; docname=DEPLOY.md ;;
    lossmon_client) name=lossmon-client; doc=client/README.md; docname=CLIENT.md ;;
    *) echo "unknown component: $component" >&2; exit 1 ;;
  esac
  base="${name}_${version}_${os}_${arch}"
  stage="$work/$base"
  mkdir -p "$stage"
  install -m 755 "$dir/$name" "$stage/$name"
  install -m 644 LICENSE-APACHE LICENSE-MIT README.md "$stage/"
  install -m 644 "$doc" "$stage/$docname"
  tar --sort=name --owner=0 --group=0 --numeric-owner --mtime='1970-01-01 00:00:00Z' \
    -C "$work" -cf - "$base" | gzip -n -9 > "$out/$base.tar.gz"
done

(cd "$out" && sha256sum -- *.tar.gz > SHA256SUMS)
