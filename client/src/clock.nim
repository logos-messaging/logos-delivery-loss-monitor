{.push raises: [].}

import std/times

proc nowMs*(): int64 =
  ## Wall-clock Unix time in milliseconds.
  let t = getTime()
  t.toUnix() * 1000 + int64(t.nanosecond() div 1_000_000)
