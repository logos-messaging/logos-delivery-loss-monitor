## State shared by the producers (publisher, receiver, heartbeat) and the reporter.

{.push raises: [].}

import ./[buffer, records]

type
  Counters* = object
    missedTicks*: int64
    foreign*: int64
    unverifiedProbe*: int64
    replayed*: int64

  ClientState* = ref object
    buffer*: RecordBuffer
    counters*: Counters

func newClientState*(bufferRecords: int): ClientState =
  ClientState(buffer: RecordBuffer.init(bufferRecords))

proc enqueue*(s: ClientState, r: Record) =
  s.buffer.push(r)
