## Receipt bookkeeping for the receiver: which probes were seen, and how many receipt
## records each one may still produce.

{.push raises: [].}

import std/[deques, tables]

const
  MaxReceiptsPerProbe* = 4
  DefaultCapacity* = 50_000

type
  ReceiptVerdict* {.pure.} = enum
    Report ## emit a receipt record
    ReportOverflow ## emit one record flagged `overflow`
    Replayed ## beyond the cap: count only
    Own ## a probe this client published itself: ignore

  ProbeEntry = object
    seenAtMs: int64
    own: bool
    reported: int
    overflowSent: bool

  ReceiptTable* = object
    entries: Table[string, ProbeEntry]
    order: Deque[(string, int64)]
    capacity: int
    ttlMs: int64

func init*(T: type ReceiptTable, ttlMs: int64, capacity = DefaultCapacity): T =
  ReceiptTable(
    entries: initTable[string, ProbeEntry](),
    order: initDeque[(string, int64)](),
    capacity: max(capacity, 1),
    ttlMs: ttlMs,
  )

func len*(t: ReceiptTable): int =
  t.entries.len

func prune*(t: var ReceiptTable, nowMs: int64) =
  while t.order.len > 0:
    let (id, at) = t.order.peekFirst()
    let expired = nowMs - at > t.ttlMs
    if not expired and t.entries.len <= t.capacity:
      break
    discard t.order.popFirst()
    if t.entries.getOrDefault(id).seenAtMs == at:
      t.entries.del(id)

func add(t: var ReceiptTable, probeId: string, nowMs: int64, own: bool) =
  t.entries[probeId] = ProbeEntry(seenAtMs: nowMs, own: own)
  t.order.addLast((probeId, nowMs))
  t.prune(nowMs)

func markOwn*(t: var ReceiptTable, probeId: string, nowMs: int64) =
  t.add(probeId, nowMs, own = true)

func onReceipt*(t: var ReceiptTable, probeId: string, nowMs: int64): ReceiptVerdict =
  t.prune(nowMs)
  if probeId notin t.entries:
    t.add(probeId, nowMs, own = false)
  var entry = t.entries.getOrDefault(probeId)
  if entry.own:
    return ReceiptVerdict.Own
  if entry.reported < MaxReceiptsPerProbe:
    inc entry.reported
    t.entries[probeId] = entry
    return ReceiptVerdict.Report
  if not entry.overflowSent:
    entry.overflowSent = true
    t.entries[probeId] = entry
    return ReceiptVerdict.ReportOverflow
  ReceiptVerdict.Replayed
