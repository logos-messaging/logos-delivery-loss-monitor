{.used.}

import std/strutils
import results, unittest2
import ../src/pending

func entry(sendAt: int64 = 1000): PendingSend =
  PendingSend(probeId: "0x" & "11".repeat(16), topic: "/t", sendAtMs: sendAt)

func ev(kind: SendEventKind, at: int64, hash = "0xaa", error = ""): SendEvent =
  SendEvent(kind: kind, hash: hash, error: error, atMs: at)

suite "pending sends":
  test "a propagated event settles the send":
    var p = PendingSends.init(10, 1800)
    check p.register("r1", entry()).isNone()
    let o = p.onEvent("r1", ev(SendEventKind.Propagated, 1500))
    check o.isSome()
    check o.get().outcome == "propagated"
    check o.get().hash == "0xaa"
    check o.get().atMs == 1500
    check o.get().sendAtMs == 1000
    check p.len == 0

  test "sent and failed events map to their outcomes":
    var p = PendingSends.init(10, 1800)
    discard p.register("r1", entry())
    check p.onEvent("r1", ev(SendEventKind.Sent, 1500)).get().outcome == "sent"
    discard p.register("r2", entry())
    let failed = p.onEvent("r2", ev(SendEventKind.Failed, 1500, error = "boom"))
    check failed.get().outcome == "error"
    check failed.get().error == "boom"

  test "the first settling event wins and later ones are ignored":
    var p = PendingSends.init(10, 1800)
    discard p.register("r1", entry())
    check p.onEvent("r1", ev(SendEventKind.Propagated, 1500)).isSome()
    check p.onEvent("r1", ev(SendEventKind.Failed, 1600)).isNone()
    check p.len == 0

  test "a queued event marks the send and does not settle it":
    var p = PendingSends.init(10, 1800)
    discard p.register("r1", entry())
    check p.onEvent("r1", ev(SendEventKind.Queued, 1100)).isNone()
    let o = p.onEvent("r1", ev(SendEventKind.Sent, 2000))
    check o.get().queued
    check p.len == 0

  test "events that arrive before register are replayed":
    var p = PendingSends.init(10, 1800)
    check p.onEvent("r1", ev(SendEventKind.Queued, 1001)).isNone()
    let o = p.register("r1", entry())
    check o.isNone()
    check p.len == 1
    let settled = p.onEvent("r1", ev(SendEventKind.Sent, 2000))
    check settled.get().queued

  test "a settling event that arrived early settles at register":
    var p = PendingSends.init(10, 1800)
    discard p.onEvent("r1", ev(SendEventKind.Propagated, 1005))
    let o = p.register("r1", entry())
    check o.isSome()
    check o.get().outcome == "propagated"
    check p.len == 0

  test "an outcome is never earlier than the send":
    var p = PendingSends.init(10, 1800)
    discard p.register("r1", entry(5000))
    check p.onEvent("r1", ev(SendEventKind.Sent, 4000)).get().atMs == 5000

  test "the table reports full at capacity":
    var p = PendingSends.init(2, 1800)
    discard p.register("a", entry())
    check not p.isFull()
    discard p.register("b", entry())
    check p.isFull()

  test "the early table is bounded":
    var p = PendingSends.init(10, 1800)
    for i in 0 ..< EarlyCapacity + 50:
      discard p.onEvent("e" & $i, ev(SendEventKind.Queued, 1000))
    let swept = p.sweep(1000 + EarlyTtlMs + 1)
    check swept.timeouts.len == 0
    check p.register("e0", entry()).isNone()

  test "stale early events are swept":
    var p = PendingSends.init(10, 1800)
    discard p.onEvent("r1", ev(SendEventKind.Propagated, 1000))
    discard p.sweep(1000 + EarlyTtlMs + 1)
    check p.register("r1", entry()).isNone()

  test "an old send with a known hash times out":
    var p = PendingSends.init(10, 10)
    discard p.register("r1", entry(1000))
    discard p.onEvent("r1", ev(SendEventKind.Queued, 1100))
    let swept = p.sweep(1000 + 10_000 + OutcomeGraceMs + 1)
    check swept.timeouts.len == 1
    check swept.timeouts[0].outcome == "timeout"
    check swept.timeouts[0].hash == "0xaa"
    check swept.timeouts[0].queued
    check p.len == 0

  test "an old send without a hash is counted as unreported":
    var p = PendingSends.init(10, 10)
    discard p.register("r1", entry(1000))
    let swept = p.sweep(1000 + 10_000 + OutcomeGraceMs + 1)
    check swept.timeouts.len == 0
    check swept.unreported == 1

  test "a recent send is left alone":
    var p = PendingSends.init(10, 1800)
    discard p.register("r1", entry(1000))
    let swept = p.sweep(2000)
    check swept.timeouts.len == 0
    check swept.unreported == 0
    check p.len == 1
