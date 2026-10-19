## Turns received messages into `receive` records, and counts the ones that are not probes.

{.push raises: [].}

import std/sets
import chronicles, results
import logos_delivery
import ./[clock, keys, probe, receipts, records, session, state]

logScope:
  topics = "lossmon receiver"

type Receiver* = ref object
  state: ClientState
  session: Session
  topics: HashSet[string]
  receipts: ReceiptTable

proc newReceiver*(state: ClientState, session: Session, topics: seq[string]): Receiver =
  Receiver(
    state: state,
    session: session,
    topics: topics.toHashSet(),
    receipts: ReceiptTable.init(session.cfg.receiptMaxAgeMs),
  )

proc markOwn*(r: Receiver, probeId: string, nowMs: int64) =
  ## Remembers a probe this client published, so its own copy is not reported.
  r.receipts.markOwn(probeId, nowMs)

proc handle(r: Receiver, e: MessageReceivedEvent) =
  if e.message.contentTopic notin r.topics:
    return
  let at = nowMs()
  let msgTsMs = e.message.timestamp div 1_000_000
  if at - msgTsMs > r.session.cfg.receiptMaxAgeMs:
    return
  let decoded = inspectProbe(e.message.payload, r.session.keys.verifying(at))
  case decoded.kind
  of ProbeKind.Foreign:
    inc r.state.counters.foreign
    return
  of ProbeKind.Unverified:
    inc r.state.counters.unverifiedProbe
    return
  of ProbeKind.Verified:
    discard
  let verdict = r.receipts.onReceipt(decoded.id.toHex0x(), at)
  case verdict
  of ReceiptVerdict.Own:
    return
  of ReceiptVerdict.Replayed:
    inc r.state.counters.replayed
    return
  of ReceiptVerdict.Report, ReceiptVerdict.ReportOverflow:
    r.state.enqueue(
      Record(
        kind: RecordKind.Receive,
        atMs: at,
        probeId: decoded.id.toHex0x(),
        hash: e.messageHash,
        topic: e.message.contentTopic,
        source: $e.source,
        msgTsMs: msgTsMs,
        overflow: verdict == ReceiptVerdict.ReportOverflow,
      )
    )

proc listen*(r: Receiver, client: MessagingClient): Result[void, string] =
  discard MessageReceivedEvent.listen(
    client.brokerCtx,
    proc(e: MessageReceivedEvent) {.async: (raises: []).} =
      r.handle(e),
  ).valueOr:
    return err("cannot listen for MessageReceivedEvent: " & error)
  ok()
