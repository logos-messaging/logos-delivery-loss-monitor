{.used.}

import unittest2
import ../src/[buffer, records, state]

func publish(at: int64): Record =
  Record(kind: RecordKind.Publish, atMs: at)

suite "client state":
  test "records are queued until the state is closed":
    let s = newClientState(10)
    s.enqueue(publish(1))
    s.enqueue(publish(2))
    check s.buffer.len == 2
    s.close()
    s.enqueue(publish(3))
    s.enqueue(Record(kind: RecordKind.End, atMs: 4))
    check s.buffer.len == 2
