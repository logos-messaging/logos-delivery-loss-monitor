## Bounded record queue. Sequence numbers are assigned on enqueue, so a record dropped
## for lack of room shows up on the server as a gap in `seq`.

{.push raises: [].}

import std/deques
import ./records

type RecordBuffer* = object
  items: Deque[Record]
  capacity: int
  nextSeq: int64
  droppedTotal*: int64 ## cumulative records dropped to make room

func init*(T: type RecordBuffer, capacity: int): T =
  RecordBuffer(items: initDeque[Record](), capacity: max(capacity, 1), nextSeq: 1)

func len*(b: RecordBuffer): int =
  b.items.len

func push*(b: var RecordBuffer, record: Record): int64 {.discardable.} =
  ## Enqueues `record` with the next sequence number and returns that number.
  if b.items.len >= b.capacity:
    discard b.items.popFirst()
    inc b.droppedTotal
  var r = record
  r.seq = b.nextSeq
  inc b.nextSeq
  b.items.addLast(r)
  r.seq

func peek*(b: RecordBuffer, maxCount: int): seq[Record] =
  ## The oldest `maxCount` records, left in the queue.
  let n = min(maxCount, b.items.len)
  result = newSeqOfCap[Record](n)
  for i in 0 ..< n:
    result.add(b.items[i])

func ack*(b: var RecordBuffer, upToSeq: int64): int {.discardable.} =
  ## Removes every record with `seq <= upToSeq` and returns how many went.
  while b.items.len > 0 and b.items[0].seq <= upToSeq:
    discard b.items.popFirst()
    inc result

func drop*(b: var RecordBuffer, count: int) =
  ## Discards the oldest `count` records without counting them as dropped for lack of room.
  for _ in 0 ..< min(count, b.items.len):
    discard b.items.popFirst()
