## Probe payload: marker, version, probe id and an HMAC tag over them, then random padding.
##
##   offset  len  field
##   0       4    "LMP1"
##   4       1    version = 1
##   5       16   probe id, from the OS CSPRNG
##   21      16   HMAC-SHA256(probe_key, payload[0..20])[0..15]
##   37      n    random padding up to the configured payload size

{.push raises: [].}

import std/sysrand
import results, stew/byteutils, nimcrypto/[hmac, sha2]

const
  ProbeMarker* = [byte('L'), byte('M'), byte('P'), byte('1')]
  ProbeVersion* = 1'u8
  ProbeIdLen* = 16
  ProbeTagLen* = 16
  ProbeKeyLen* = 32
  ProbeAuthLen* = ProbeMarker.len + 1 + ProbeIdLen
  ProbeMinLen* = ProbeAuthLen + ProbeTagLen

type
  ProbeId* = array[ProbeIdLen, byte]
  ProbeKey* = array[ProbeKeyLen, byte]

  ProbeKind* {.pure.} = enum
    Foreign ## no probe marker
    Unverified ## marker parses but the tag does not verify
    Verified

  ProbeDecode* = object
    kind*: ProbeKind
    id*: ProbeId ## set only when `kind` is `Verified`

func ctEqual*(a, b: openArray[byte]): bool =
  ## Compares in time that depends only on the length, never on the contents.
  if a.len != b.len:
    return false
  var diff = 0'u8
  for i in 0 ..< a.len:
    diff = diff or (a[i] xor b[i])
  diff == 0

func probeTag(key: ProbeKey, authenticated: openArray[byte]): array[ProbeTagLen, byte] =
  let mac = sha256.hmac(key, authenticated)
  for i in 0 ..< ProbeTagLen:
    result[i] = mac.data[i]

proc newProbeId*(): Result[ProbeId, string] =
  var id: ProbeId
  if not urandom(id):
    return err("the OS random source failed")
  ok(id)

proc encodeProbe*(
    key: ProbeKey, id: ProbeId, payloadBytes: int
): Result[seq[byte], string] =
  ## Builds a payload of exactly `payloadBytes` bytes.
  if payloadBytes < ProbeMinLen:
    return err("payload size is below " & $ProbeMinLen & " bytes")
  var payload = newSeq[byte](payloadBytes)
  for i in 0 ..< ProbeMarker.len:
    payload[i] = ProbeMarker[i]
  payload[ProbeMarker.len] = ProbeVersion
  for i in 0 ..< ProbeIdLen:
    payload[ProbeMarker.len + 1 + i] = id[i]
  let tag = probeTag(key, payload.toOpenArray(0, ProbeAuthLen - 1))
  for i in 0 ..< ProbeTagLen:
    payload[ProbeAuthLen + i] = tag[i]
  if payloadBytes > ProbeMinLen:
    if not urandom(payload.toOpenArray(ProbeMinLen, payloadBytes - 1)):
      return err("the OS random source failed")
  ok(payload)

func inspectProbe*(
    payload: openArray[byte], keys: openArray[ProbeKey]
): ProbeDecode {.raises: [].} =
  ## Classifies a payload. Reads nothing beyond `payload.len`.
  if payload.len <= ProbeMarker.len:
    return ProbeDecode(kind: ProbeKind.Foreign)
  for i in 0 ..< ProbeMarker.len:
    if payload[i] != ProbeMarker[i]:
      return ProbeDecode(kind: ProbeKind.Foreign)
  if payload[ProbeMarker.len] != ProbeVersion or payload.len < ProbeMinLen:
    return ProbeDecode(kind: ProbeKind.Unverified)
  var matched = false
  for key in keys:
    let tag = probeTag(key, payload.toOpenArray(0, ProbeAuthLen - 1))
    matched =
      ctEqual(tag, payload.toOpenArray(ProbeAuthLen, ProbeMinLen - 1)) or matched
  if not matched:
    return ProbeDecode(kind: ProbeKind.Unverified)
  var id: ProbeId
  for i in 0 ..< ProbeIdLen:
    id[i] = payload[ProbeMarker.len + 1 + i]
  ProbeDecode(kind: ProbeKind.Verified, id: id)

func decodeProbe*(
    payload: openArray[byte], keys: openArray[ProbeKey]
): Opt[ProbeId] {.raises: [].} =
  let decoded = inspectProbe(payload, keys)
  if decoded.kind == ProbeKind.Verified:
    Opt.some(decoded.id)
  else:
    Opt.none(ProbeId)

func toHex0x*(id: ProbeId): string =
  "0x" & id.toHex()

func parseProbeKey*(hex: string): Result[ProbeKey, string] =
  ## Accepts the `0x`-prefixed 64-digit form served by `/v1/config`.
  if hex.len != 2 + 2 * ProbeKeyLen or hex[0] != '0' or hex[1] != 'x':
    return err("probe key must be 0x followed by 64 hex digits")
  var key: ProbeKey
  try:
    hexToByteArray(hex[2 ..^ 1], key)
  except ValueError:
    return err("probe key is not valid hex")
  ok(key)
