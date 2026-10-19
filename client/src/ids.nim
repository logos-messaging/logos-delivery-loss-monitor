## Client and session identifiers.

{.push raises: [].}

import std/[os, sysrand, strutils]
import results, stew/byteutils

const ClientIdFile = "client_id"

func isUuid*(s: string): bool =
  if s.len != 36:
    return false
  for i, c in s:
    if i in [8, 13, 18, 23]:
      if c != '-':
        return false
    elif c notin {'0' .. '9', 'a' .. 'f'}:
      return false
  true

proc newUuid4*(): Result[string, string] =
  ## Lowercase UUIDv4 from the OS CSPRNG.
  var b: array[16, byte]
  if not urandom(b):
    return err("the OS random source failed")
  b[6] = (b[6] and 0x0f) or 0x40
  b[8] = (b[8] and 0x3f) or 0x80
  let h = b.toHex()
  ok(
    h[0 .. 7] & "-" & h[8 .. 11] & "-" & h[12 .. 15] & "-" & h[16 .. 19] & "-" &
      h[20 .. 31]
  )

proc loadOrCreateClientId*(dataDir: string): Result[string, string] =
  ## The id lives in `dataDir` so a restart keeps it.
  let path = dataDir / ClientIdFile
  try:
    createDir(dataDir)
    if fileExists(path):
      let existing = readFile(path).strip()
      if not isUuid(existing):
        return err(path & " does not hold a UUID")
      return ok(existing)
    let fresh = ?newUuid4()
    writeFile(path, fresh & "\n")
    setFilePermissions(path, {fpUserRead, fpUserWrite})
    ok(fresh)
  except CatchableError as e:
    err("cannot use the data directory: " & e.msg)
