{.used.}

import unittest2
import ../src/receipts

const Ttl = 600_000'i64

suite "receipt table":
  test "the first four receipts of a probe are reported":
    var t = ReceiptTable.init(Ttl)
    for _ in 1 .. MaxReceiptsPerProbe:
      check t.onReceipt("p1", 1000) == ReceiptVerdict.Report

  test "the fifth is reported once as overflow, then only counted":
    var t = ReceiptTable.init(Ttl)
    for _ in 1 .. MaxReceiptsPerProbe:
      discard t.onReceipt("p1", 1000)
    check t.onReceipt("p1", 1000) == ReceiptVerdict.ReportOverflow
    check t.onReceipt("p1", 1000) == ReceiptVerdict.Replayed
    check t.onReceipt("p1", 1000) == ReceiptVerdict.Replayed

  test "probes are counted separately":
    var t = ReceiptTable.init(Ttl)
    for _ in 1 .. 10:
      discard t.onReceipt("p1", 1000)
    check t.onReceipt("p2", 1000) == ReceiptVerdict.Report

  test "own probes are ignored":
    var t = ReceiptTable.init(Ttl)
    t.markOwn("p1", 1000)
    check t.onReceipt("p1", 1100) == ReceiptVerdict.Own
    check t.onReceipt("p1", 1200) == ReceiptVerdict.Own

  test "entries expire after the time to live":
    var t = ReceiptTable.init(ttlMs = 1000)
    for _ in 1 .. 6:
      discard t.onReceipt("p1", 0)
    check t.onReceipt("p1", 500) == ReceiptVerdict.Replayed
    check t.onReceipt("p1", 1500) == ReceiptVerdict.Report

  test "the table is bounded and evicts the oldest":
    var t = ReceiptTable.init(Ttl, capacity = 3)
    for i in 0 ..< 10:
      discard t.onReceipt("p" & $i, int64(i))
    check t.len <= 3
    check t.onReceipt("p0", 20) == ReceiptVerdict.Report

  test "a recent probe survives eviction of older ones":
    var t = ReceiptTable.init(Ttl, capacity = 3)
    for _ in 1 .. 6:
      discard t.onReceipt("keep", 100)
    for i in 0 ..< 2:
      discard t.onReceipt("p" & $i, int64(200 + i))
    check t.onReceipt("keep", 300) == ReceiptVerdict.Replayed
