## Records the client uploads to `POST /v1/sessions/{id}/records`.

{.push raises: [].}

import std/json

type
  RecordKind* {.pure.} = enum
    Heartbeat = "heartbeat"
    Publish = "publish"
    Receive = "receive"
    End = "end"

  Record* = object
    ## One flat object for every kind; `toJson` emits only the fields its kind carries.
    seq*: int64 ## assigned by `RecordBuffer.push`
    atMs*: int64
    kind*: RecordKind

    conn*: string
    subscribed*: seq[string]
    droppedTotal*: int64
    missedTicksTotal*: int64
    foreignTotal*: int64
    unverifiedProbeTotal*: int64
    replayedTotal*: int64

    sendAtMs*: int64
    probeId*: string
    hash*: string
    topic*: string
    outcome*: string
    error*: string
    queued*: bool

    source*: string
    msgTsMs*: int64
    overflow*: bool

func toJson*(r: Record): JsonNode =
  result = %*{"seq": r.seq, "type": $r.kind, "at_ms": r.atMs}
  case r.kind
  of RecordKind.Heartbeat:
    result["conn"] = %r.conn
    result["subscribed"] = %r.subscribed
    result["dropped_total"] = %r.droppedTotal
    result["missed_ticks_total"] = %r.missedTicksTotal
    result["foreign_total"] = %r.foreignTotal
    result["unverified_probe_total"] = %r.unverifiedProbeTotal
    result["replayed_total"] = %r.replayedTotal
  of RecordKind.Publish:
    result["send_at_ms"] = %r.sendAtMs
    result["probe_id"] = %r.probeId
    result["hash"] = %r.hash
    result["topic"] = %r.topic
    result["outcome"] = %r.outcome
    result["error"] = %r.error
    result["queued"] = %r.queued
  of RecordKind.Receive:
    result["probe_id"] = %r.probeId
    result["hash"] = %r.hash
    result["topic"] = %r.topic
    result["source"] = %r.source
    result["msg_ts_ms"] = %r.msgTsMs
    result["overflow"] = %r.overflow
  of RecordKind.End:
    discard

func batchBody*(sentAtMs: int64, records: openArray[Record]): string =
  var items = newJArray()
  for r in records:
    items.add(r.toJson())
  $(%*{"sent_at_ms": sentAtMs, "records": items})
