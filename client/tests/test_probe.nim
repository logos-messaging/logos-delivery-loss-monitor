{.used.}

import std/[sysrand, strutils]
import results, unittest2
import ../src/probe

const PayloadSize = 256

let
  key: ProbeKey = block:
    var k: ProbeKey
    for i in 0 ..< k.len:
      k[i] = byte(i + 1)
    k
  otherKey: ProbeKey = block:
    var k: ProbeKey
    for i in 0 ..< k.len:
      k[i] = byte(200 - i)
    k

suite "probe payload":
  test "encode then decode returns the probe id":
    let id = newProbeId().get()
    let payload = encodeProbe(key, id, PayloadSize).get()
    check payload.len == PayloadSize
    check decodeProbe(payload, [key]) == Opt.some(id)

  test "the smallest payload carries header and tag only":
    let id = newProbeId().get()
    let payload = encodeProbe(key, id, ProbeMinLen).get()
    check payload.len == ProbeMinLen
    check decodeProbe(payload, [key]) == Opt.some(id)

  test "a payload below the minimum is refused":
    check encodeProbe(key, default(ProbeId), ProbeMinLen - 1).isErr()

  test "every length from 0 to 64 decodes without raising":
    for len in 0 .. 64:
      var buf = newSeq[byte](len)
      if len > 0:
        check urandom(buf)
      check decodeProbe(buf, [key]).isNone()
      let kind = inspectProbe(buf, [key]).kind
      check kind != ProbeKind.Verified

  test "truncating a valid payload at every length never verifies":
    let payload = encodeProbe(key, newProbeId().get(), PayloadSize).get()
    for len in 1 ..< ProbeMinLen:
      check inspectProbe(payload.toOpenArray(0, len - 1), [key]).kind !=
        ProbeKind.Verified

  test "ten thousand random buffers never verify":
    for _ in 0 ..< 10_000:
      let len = int(urandom(1)[0]) + 1
      let buf = urandom(len)
      check decodeProbe(buf, [key]).isNone()

  test "random buffers that start with the marker are unverified, not foreign":
    for _ in 0 ..< 200:
      var buf = urandom(ProbeMinLen + 10)
      for i in 0 ..< ProbeMarker.len:
        buf[i] = ProbeMarker[i]
      buf[ProbeMarker.len] = ProbeVersion
      check inspectProbe(buf, [key]).kind == ProbeKind.Unverified

  test "flipping any single byte of the authenticated header or the tag breaks it":
    let payload = encodeProbe(key, newProbeId().get(), PayloadSize).get()
    for i in 0 ..< ProbeMinLen:
      var bad = payload
      bad[i] = bad[i] xor 0x01
      check decodeProbe(bad, [key]).isNone()

  test "flipping every bit of the tag breaks it":
    let payload = encodeProbe(key, newProbeId().get(), PayloadSize).get()
    for i in ProbeAuthLen ..< ProbeMinLen:
      for bit in 0 ..< 8:
        var bad = payload
        bad[i] = bad[i] xor byte(1 shl bit)
        check decodeProbe(bad, [key]).isNone()

  test "padding is not authenticated":
    var payload = encodeProbe(key, newProbeId().get(), PayloadSize).get()
    payload[PayloadSize - 1] = payload[PayloadSize - 1] xor 0xff
    check decodeProbe(payload, [key]).isSome()

  test "a payload without the marker is foreign":
    check inspectProbe(@[byte 1, 2, 3, 4, 5, 6], [key]).kind == ProbeKind.Foreign
    check inspectProbe(newSeq[byte](0), [key]).kind == ProbeKind.Foreign

  test "a wrong version is unverified":
    var payload = encodeProbe(key, newProbeId().get(), PayloadSize).get()
    payload[ProbeMarker.len] = 2
    check inspectProbe(payload, [key]).kind == ProbeKind.Unverified

  test "a probe made with another key is unverified":
    let payload = encodeProbe(otherKey, newProbeId().get(), PayloadSize).get()
    check inspectProbe(payload, [key]).kind == ProbeKind.Unverified

  test "the previous key still verifies when listed":
    let id = newProbeId().get()
    let payload = encodeProbe(otherKey, id, PayloadSize).get()
    check decodeProbe(payload, [key, otherKey]) == Opt.some(id)

  test "no keys verifies nothing":
    let payload = encodeProbe(key, newProbeId().get(), PayloadSize).get()
    check decodeProbe(payload, newSeq[ProbeKey]()).isNone()

  test "probe ids and padding are random":
    let a = encodeProbe(key, newProbeId().get(), PayloadSize).get()
    let b = encodeProbe(key, newProbeId().get(), PayloadSize).get()
    check a != b
    check a[ProbeMinLen ..^ 1] != b[ProbeMinLen ..^ 1]

  test "ctEqual compares contents and lengths":
    check ctEqual([byte 1, 2, 3], [byte 1, 2, 3])
    check not ctEqual([byte 1, 2, 3], [byte 1, 2, 4])
    check not ctEqual([byte 1, 2, 3], [byte 1, 2])
    check ctEqual(newSeq[byte](0), newSeq[byte](0))

  test "probe key parsing":
    check parseProbeKey("0x" & "ab".repeat(32)).isOk()
    check parseProbeKey("ab".repeat(32)).isErr()
    check parseProbeKey("0x" & "zz".repeat(32)).isErr()
    check parseProbeKey("0x" & "ab".repeat(31)).isErr()

  test "probe id hex form":
    var id: ProbeId
    id[0] = 0xab
    check id.toHex0x() == "0xab" & "00".repeat(15)
