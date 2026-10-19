#!/bin/sh
# Rebuilds the C libraries of the Nimble dependencies with a portable CPU baseline.
# Usage: native-deps.sh <nimbledeps-dir>
set -eu

pkgs=${1:?nimbledeps dir}/pkgs2
pkg() { ls -dt "$pkgs"/"$1"-* | head -1; }

march=
[ "$(uname -m)" = x86_64 ] && march=-mssse3

bearssl=$(pkg bearssl)
make -C "$bearssl/bearssl/csources" CFLAGS="-W -Wall -Os -fPIC $march" lib

nat=$(pkg nat_traversal)
rm -f "$nat/vendor/miniupnp/miniupnpc/build/libminiupnpc.a" "$nat/vendor/libnatpmp-upstream/libnatpmp.a"
make -C "$nat/vendor/miniupnp/miniupnpc" CFLAGS="-Os -fPIC $march" build/libminiupnpc.a
make -C "$nat/vendor/libnatpmp-upstream" \
  CFLAGS="-Wall -Wno-cpp -Os -fPIC $march -DENABLE_STRNATPMPERR -DNATPMP_MAX_RETRIES=4" libnatpmp.a

# Nimble strips tests/, but Leopard's CMakeLists still lists two sources there.
tests=$(pkg leopard)/vendor/leopard/tests
mkdir -p "$tests"
for stub in benchmark experiments; do
  [ -f "$tests/$stub.cpp" ] || echo 'int main() { return 0; }' > "$tests/$stub.cpp"
done
