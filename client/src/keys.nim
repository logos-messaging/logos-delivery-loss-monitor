## The run's probe key and, for a while after a rotation, the previous one.

{.push raises: [].}

import results
import ./probe

const PreviousKeyTtlMs* = 11 * 60 * 1000'i64 ## default window plus grace

type ProbeKeys* = object
  ready: bool
  current: ProbeKey
  previous: Opt[ProbeKey]
  previousUntilMs: int64

func isReady*(k: ProbeKeys): bool =
  k.ready

func current*(k: ProbeKeys): ProbeKey =
  ## Only meaningful once `isReady`.
  k.current

func rotate*(k: var ProbeKeys, key: ProbeKey, nowMs: int64) =
  ## The first call sets the key. Later calls with a different key demote the old one.
  if not k.ready:
    k.ready = true
    k.current = key
    return
  if ctEqual(key, k.current):
    return
  k.previous = Opt.some(k.current)
  k.previousUntilMs = nowMs + PreviousKeyTtlMs
  k.current = key

func verifying*(k: ProbeKeys, nowMs: int64): seq[ProbeKey] =
  ## Keys a receiver accepts now: the current one, plus the previous until it expires.
  if not k.ready:
    return
  result = @[k.current]
  if k.previous.isSome() and nowMs <= k.previousUntilMs:
    result.add(k.previous.get())
