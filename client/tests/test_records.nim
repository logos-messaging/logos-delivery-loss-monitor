{.used.}

import std/[json, strutils]
import unittest2
import ../src/records

suite "record json":
  test "a heartbeat carries its counters and nothing of the other kinds":
    let j = Record(
      seq: 7,
      kind: RecordKind.Heartbeat,
      atMs: 1000,
      conn: "connected",
      subscribed: @["/t"],
      droppedTotal: 1,
      missedTicksTotal: 2,
      foreignTotal: 3,
      unverifiedProbeTotal: 4,
      replayedTotal: 5,
    ).toJson()
    check j["type"].getStr() == "heartbeat"
    check j["seq"].getInt() == 7
    check j["at_ms"].getInt() == 1000
    check j["conn"].getStr() == "connected"
    check j["subscribed"][0].getStr() == "/t"
    check j["dropped_total"].getInt() == 1
    check j["missed_ticks_total"].getInt() == 2
    check j["foreign_total"].getInt() == 3
    check j["unverified_probe_total"].getInt() == 4
    check j["replayed_total"].getInt() == 5
    check not j.hasKey("hash")

  test "a publish record":
    let j = Record(
      seq: 8,
      kind: RecordKind.Publish,
      atMs: 2000,
      sendAtMs: 1900,
      probeId: "0x" & "ab".repeat(16),
      hash: "0x" & "cd".repeat(32),
      topic: "/t",
      outcome: "propagated",
      queued: true,
    ).toJson()
    check j["type"].getStr() == "publish"
    check j["send_at_ms"].getInt() == 1900
    check j["probe_id"].getStr().len == 34
    check j["hash"].getStr().len == 66
    check j["outcome"].getStr() == "propagated"
    check j["error"].getStr() == ""
    check j["queued"].getBool()
    check not j.hasKey("source")

  test "a receive record":
    let j = Record(
      seq: 9,
      kind: RecordKind.Receive,
      atMs: 3000,
      probeId: "0x" & "ab".repeat(16),
      hash: "0x" & "cd".repeat(32),
      topic: "/t",
      source: "history",
      msgTsMs: 2500,
      overflow: true,
    ).toJson()
    check j["type"].getStr() == "receive"
    check j["source"].getStr() == "history"
    check j["msg_ts_ms"].getInt() == 2500
    check j["overflow"].getBool()
    check not j.hasKey("outcome")

  test "an end record has only the common fields":
    let j = Record(seq: 10, kind: RecordKind.End, atMs: 4000).toJson()
    check j["type"].getStr() == "end"
    check j.len == 3

  test "a batch wraps records with the send time":
    let body = batchBody(5000, [Record(seq: 1, kind: RecordKind.End, atMs: 4000)])
    let j = parseJson(body)
    check j["sent_at_ms"].getInt() == 5000
    check j["records"].len == 1
