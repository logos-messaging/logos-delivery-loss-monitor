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
    closed: bool

func newClientState*(bufferRecords: int): ClientState =
  ClientState(buffer: RecordBuffer.init(bufferRecords))

proc enqueue*(s: ClientState, r: Record) =
  ## Queues `r` unless the state is closed.
  if not s.closed:
    s.buffer.push(r)

proc close*(s: ClientState) =
  ## Makes every later `enqueue` a no-op.
  s.closed = true
