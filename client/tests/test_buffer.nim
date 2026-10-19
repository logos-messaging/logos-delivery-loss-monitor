{.used.}

import std/sequtils
import unittest2
import ../src/[buffer, records]

func publish(at: int64): Record =
  Record(kind: RecordKind.Publish, atMs: at)

suite "record buffer":
  test "sequence numbers start at one and increase":
    var b = RecordBuffer.init(10)
    check b.push(publish(1)) == 1
    check b.push(publish(2)) == 2
    check b.push(publish(3)) == 3

  test "peek returns the oldest records and leaves them queued":
    var b = RecordBuffer.init(10)
    for i in 1 .. 5:
      b.push(publish(int64(i)))
    let batch = b.peek(3)
    check batch.len == 3
    check batch[0].seq == 1
    check batch[2].seq == 3
    check b.len == 5

  test "ack removes records up to the acked sequence":
    var b = RecordBuffer.init(10)
    for i in 1 .. 5:
      b.push(publish(int64(i)))
    check b.ack(3) == 3
    check b.len == 2
    check b.peek(10)[0].seq == 4

  test "an ack beyond the queue empties it":
    var b = RecordBuffer.init(10)
    b.push(publish(1))
    check b.ack(99) == 1
    check b.len == 0

  test "an ack below the head removes nothing":
    var b = RecordBuffer.init(10)
    b.push(publish(1))
    b.push(publish(2))
    discard b.ack(1)
    check b.ack(1) == 0
    check b.len == 1

  test "a full queue drops the oldest and counts it":
    var b = RecordBuffer.init(3)
    for i in 1 .. 5:
      b.push(publish(int64(i)))
    check b.len == 3
    check b.droppedTotal == 2
    check b.peek(10)[0].seq == 3

  test "a dropped record leaves a gap in the sequence":
    var b = RecordBuffer.init(2)
    for i in 1 .. 4:
      b.push(publish(int64(i)))
    let seqs = b.peek(10).mapIt(it.seq)
    check seqs == @[3'i64, 4]

  test "sequence numbers keep counting after an ack":
    var b = RecordBuffer.init(10)
    b.push(publish(1))
    discard b.ack(1)
    check b.push(publish(2)) == 2

  test "drop discards without counting":
    var b = RecordBuffer.init(10)
    for i in 1 .. 4:
      b.push(publish(int64(i)))
    b.drop(2)
    check b.len == 2
    check b.droppedTotal == 0

  test "a zero capacity still holds one record":
    var b = RecordBuffer.init(0)
    b.push(publish(1))
    check b.len == 1
