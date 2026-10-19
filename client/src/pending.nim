## Sends awaiting an outcome event, matched by request id. Events can arrive before
## `send()` has handed its request id back, so those wait in a small bounded table.

{.push raises: [].}

import std/tables
import results

const
  EarlyCapacity* = 1000
  EarlyTtlMs* = 10_000'i64
  OutcomeGraceMs* = 60_000'i64

type
  SendEventKind* {.pure.} = enum
    Queued
    Propagated
    Sent
    Failed

  SendEvent* = object
    kind*: SendEventKind
    hash*: string
    error*: string
    atMs*: int64

  PendingSend* = object
    probeId*: string
    topic*: string
    sendAtMs*: int64
    hash*: string ## known once any event has carried it
    queued*: bool

  Outcome* = object ## What the publisher turns into a `publish` record.
    probeId*: string
    topic*: string
    sendAtMs*: int64
    atMs*: int64
    hash*: string
    outcome*: string
    error*: string
    queued*: bool

  PendingSends* = object
    pending: Table[string, PendingSend]
    early: Table[string, seq[SendEvent]]
    earlyOrder: seq[string]
    capacity: int
    maxAgeMs: int64

func init*(T: type PendingSends, capacity: int, maxParkedAgeSec: int64): T =
  PendingSends(
    pending: initTable[string, PendingSend](),
    early: initTable[string, seq[SendEvent]](),
    capacity: capacity,
    maxAgeMs: maxParkedAgeSec * 1000 + OutcomeGraceMs,
  )

func len*(p: PendingSends): int =
  p.pending.len

func isFull*(p: PendingSends): bool =
  p.pending.len >= p.capacity

func outcomeOf(p: PendingSend, ev: SendEvent): Opt[Outcome] =
  let name =
    case ev.kind
    of SendEventKind.Propagated:
      "propagated"
    of SendEventKind.Sent:
      "sent"
    of SendEventKind.Failed:
      "error"
    of SendEventKind.Queued:
      return Opt.none(Outcome)
  Opt.some(
    Outcome(
      probeId: p.probeId,
      topic: p.topic,
      sendAtMs: p.sendAtMs,
      atMs: max(ev.atMs, p.sendAtMs),
      hash: if ev.hash.len > 0: ev.hash else: p.hash,
      outcome: name,
      error: ev.error,
      queued: p.queued,
    )
  )

func apply(p: var PendingSends, id: string, ev: SendEvent): Opt[Outcome] =
  ## The entry for `id` must exist.
  var entry = p.pending.getOrDefault(id)
  if ev.hash.len > 0:
    entry.hash = ev.hash
  if ev.kind == SendEventKind.Queued:
    entry.queued = true
    p.pending[id] = entry
    return Opt.none(Outcome)
  p.pending.del(id)
  outcomeOf(entry, ev)

func onEvent*(p: var PendingSends, requestId: string, ev: SendEvent): Opt[Outcome] =
  ## Returns an outcome when the event settles a registered send. An event for an
  ## unknown id is parked until `register` claims it.
  if requestId in p.pending:
    return p.apply(requestId, ev)
  if requestId notin p.early:
    if p.earlyOrder.len >= EarlyCapacity:
      p.early.del(p.earlyOrder[0])
      p.earlyOrder.delete(0)
    p.earlyOrder.add(requestId)
  p.early.mgetOrPut(requestId, @[]).add(ev)
  Opt.none(Outcome)

func register*(
    p: var PendingSends, requestId: string, entry: PendingSend
): Opt[Outcome] =
  ## Tracks a send. When events for it already arrived they are replayed in order and
  ## the first settling one is returned.
  p.pending[requestId] = entry
  let parked = p.early.getOrDefault(requestId)
  if requestId in p.early:
    p.early.del(requestId)
    let idx = p.earlyOrder.find(requestId)
    if idx >= 0:
      p.earlyOrder.delete(idx)
  for ev in parked:
    if requestId notin p.pending:
      break
    let settled = p.apply(requestId, ev)
    if settled.isSome():
      return settled
  Opt.none(Outcome)

func sweep*(
    p: var PendingSends, nowMs: int64
): tuple[timeouts: seq[Outcome], unreported: int] =
  ## Settles sends older than the node's parking limit as `timeout`. A send that never
  ## carried a hash cannot be reported and counts as `unreported`.
  var expired: seq[(string, PendingSend)]
  for id, entry in p.pending:
    if nowMs - entry.sendAtMs > p.maxAgeMs:
      expired.add((id, entry))
  for (id, entry) in expired:
    p.pending.del(id)
    if entry.hash.len == 0:
      inc result.unreported
    else:
      result.timeouts.add(
        Outcome(
          probeId: entry.probeId,
          topic: entry.topic,
          sendAtMs: entry.sendAtMs,
          atMs: nowMs,
          hash: entry.hash,
          outcome: "timeout",
          queued: entry.queued,
        )
      )
  var stale: seq[string]
  for id in p.earlyOrder:
    let events = p.early.getOrDefault(id)
    if events.len == 0 or nowMs - events[0].atMs > EarlyTtlMs:
      stale.add(id)
  for id in stale:
    p.early.del(id)
    p.earlyOrder.delete(p.earlyOrder.find(id))
