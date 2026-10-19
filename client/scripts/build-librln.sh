#!/bin/sh
# Builds the stateless librln static library from a pinned zerokit commit.
# Usage: build-librln.sh <zerokit-commit> <output-file>
set -eu

commit=${1:?zerokit commit}
out=${2:?output file}
src=$(dirname "$out")/zerokit

mkdir -p "$src"
if [ "$(git -C "$src" rev-parse HEAD 2>/dev/null || true)" != "$commit" ]; then
  rm -rf "$src"
  mkdir -p "$src"
  git -C "$src" init -q
  git -C "$src" fetch -q --depth 1 https://github.com/vacp2p/zerokit "$commit"
  git -C "$src" checkout -q FETCH_HEAD
fi

cargo build --release --locked -p rln --manifest-path "$src/rln/Cargo.toml" \
  --no-default-features --features stateless
cp "$src/target/release/librln.a" "$out"
