{.used.}

import unittest2
import ../src/[keys, probe]

const Ttl = 5000'i64

func key(b: byte): ProbeKey =
  for i in 0 ..< result.len:
    result[i] = b

suite "probe keys":
  test "nothing verifies before a key is set":
    var k: ProbeKeys
    check not k.isReady()
    check k.verifying(0).len == 0

  test "the first key becomes current without a previous one":
    var k: ProbeKeys
    k.rotate(key(1), 0, Ttl)
    check k.isReady()
    check k.verifying(0) == @[key(1)]

  test "rotating to the same key changes nothing":
    var k: ProbeKeys
    k.rotate(key(1), 0, Ttl)
    k.rotate(key(1), 100, Ttl)
    check k.verifying(100) == @[key(1)]

  test "a rotated-out key verifies until it expires":
    var k: ProbeKeys
    k.rotate(key(1), 0, Ttl)
    k.rotate(key(2), 1000, Ttl)
    check k.current() == key(2)
    check k.verifying(1000) == @[key(2), key(1)]
    check k.verifying(1000 + Ttl) == @[key(2), key(1)]
    check k.verifying(1000 + Ttl + 1) == @[key(2)]
