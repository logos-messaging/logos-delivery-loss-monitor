## Publishes probes at a fixed rate per topic and turns send events into `publish` records.

{.push raises: [].}

import chronicles, chronos, results
import logos_delivery
import ./[clock, keys, pending, probe, records, scheduler, session, shutdown, state]

logScope:
  topics = "lossmon publisher"

const
  SweepInterval = 10.seconds
  IdlePoll = 500.milliseconds

type
  OwnProbeHook* = proc(probeId: string, nowMs: int64) {.gcsafe, raises: [].}

  Publisher* = ref object
    client: MessagingClient
    state: ClientState
    session: Session
    topics: seq[string]
    ratePerTopic: float
    payloadBytes: int
    pending: PendingSends
    onOwnProbe: OwnProbeHook

proc newPublisher*(
    client: MessagingClient,
    state: ClientState,
    session: Session,
    topics: seq[string],
    ratePerTopic: float,
    payloadBytes: int,
    sendQueueCapacity: int,
    maxParkedAgeSec: int64,
    onOwnProbe: OwnProbeHook,
): Publisher =
  Publisher(
    client: client,
    state: state,
    session: session,
    topics: topics,
    ratePerTopic: ratePerTopic,
    payloadBytes: payloadBytes,
    pending: PendingSends.init(sendQueueCapacity, maxParkedAgeSec),
    onOwnProbe: onOwnProbe,
  )

proc emit(p: Publisher, o: Outcome) =
  p.state.enqueue(
    Record(
      kind: RecordKind.Publish,
      atMs: o.atMs,
      sendAtMs: o.sendAtMs,
      probeId: o.probeId,
      hash: o.hash,
      topic: o.topic,
      outcome: o.outcome,
      error: o.error,
      queued: o.queued,
    )
  )

proc onSendEvent(
    p: Publisher, requestId: RequestId, kind: PendingEventKind, hash, error: string
) =
  let settled = p.pending.onEvent(
    $requestId, SendEvent(kind: kind, hash: hash, error: error, atMs: nowMs())
  )
  if settled.isSome():
    p.emit(settled.get())

proc listen*(p: Publisher): Result[void, string] =
  ## Subscribes to the node's send events.
  let ctx = p.client.brokerCtx
  discard MessageQueuedEvent.listen(
    ctx,
    proc(e: MessageQueuedEvent) {.async: (raises: []).} =
      p.onSendEvent(e.requestId, PendingEventKind.Queued, e.messageHash, ""),
  ).valueOr:
    return err("cannot listen for MessageQueuedEvent: " & error)
  discard MessagePropagatedEvent.listen(
    ctx,
    proc(e: MessagePropagatedEvent) {.async: (raises: []).} =
      p.onSendEvent(e.requestId, PendingEventKind.Propagated, e.messageHash, ""),
  ).valueOr:
    return err("cannot listen for MessagePropagatedEvent: " & error)
  discard MessageSentEvent.listen(
    ctx,
    proc(e: MessageSentEvent) {.async: (raises: []).} =
      p.onSendEvent(e.requestId, PendingEventKind.Sent, e.messageHash, ""),
  ).valueOr:
    return err("cannot listen for MessageSentEvent: " & error)
  discard MessageErrorEvent.listen(
    ctx,
    proc(e: MessageErrorEvent) {.async: (raises: []).} =
      p.onSendEvent(e.requestId, PendingEventKind.Failed, e.messageHash, e.error),
  ).valueOr:
    return err("cannot listen for MessageErrorEvent: " & error)
  ok()

proc publishOne(p: Publisher, topic: string) {.async: (raises: [CancelledError]).} =
  if p.pending.isFull():
    inc p.state.counters.missedTicks
    return
  let probeId = newProbeId().valueOr:
    error "cannot make a probe id", reason = error
    inc p.state.counters.missedTicks
    return
  let payload = encodeProbe(p.session.keys.current(), probeId, p.payloadBytes).valueOr:
    error "cannot build a probe", reason = error
    inc p.state.counters.missedTicks
    return
  let sendAtMs = nowMs()
  let sent =
    try:
      await p.client.send(
        MessageEnvelope.init(topic, payload, ephemeral = p.session.cfg.ephemeral)
      )
    except CancelledError as e:
      raise e
    except CatchableError as e:
      Result[RequestId, string].err(e.msg)
  let requestId = sent.valueOr:
    warn "send refused", reason = error
    inc p.state.counters.missedTicks
    return
  let idHex = probeId.toHex0x()
  p.onOwnProbe(idHex, sendAtMs)
  let settled = p.pending.register(
    $requestId, PendingSend(probeId: idHex, topic: topic, sendAtMs: sendAtMs)
  )
  if settled.isSome():
    p.emit(settled.get())

proc runTopic(
    p: Publisher, topic: string, phase: Duration
) {.async: (raises: [CancelledError]).} =
  var schedule = RateSchedule.init(Moment.now() + phase, p.ratePerTopic)
  while not isRequested():
    let now = Moment.now()
    if now < schedule.nextAt():
      await sleepAsync(min(schedule.nextAt() - now, IdlePoll))
      continue
    let tick = schedule.advance(now)
    p.state.counters.missedTicks += int64(tick.missed)
    if tick.fire:
      await p.publishOne(topic)

proc runSweeper(p: Publisher) {.async: (raises: [CancelledError]).} =
  while not isRequested():
    await sleepAsync(SweepInterval)
    let swept = p.pending.sweep(nowMs())
    for o in swept.timeouts:
      p.emit(o)
    p.state.counters.missedTicks += int64(swept.unreported)

proc hasPending*(p: Publisher): bool =
  p.pending.len > 0

proc run*(p: Publisher) {.async: (raises: [CancelledError]).} =
  ## Runs one loop per topic, started at evenly spread phases, until shutdown.
  var loops: seq[Future[void].Raising([CancelledError])]
  let period = nanoseconds(int64(1_000_000_000.0 / p.ratePerTopic))
  for i, topic in p.topics:
    loops.add(p.runTopic(topic, period * i div max(p.topics.len, 1)))
  loops.add(p.runSweeper())
  try:
    for f in loops:
      await f
  except CancelledError as e:
    for f in loops:
      await f.cancelAndWait()
    raise e
