## Periodic and change-triggered `heartbeat` records.

{.push raises: [].}

import chronicles, chronos, results
import logos_delivery
import ./[clock, records, shutdown, state]

logScope:
  topics = "lossmon heartbeat"

type Heartbeat* = ref object
  node: LogosDelivery
  state: ClientState
  intervalMs: int64
  subscribed: seq[string]
  lastTriggeredMs: int64

proc newHeartbeat*(
    node: LogosDelivery, state: ClientState, intervalMs: int64, subscribed: seq[string]
): Heartbeat =
  Heartbeat(node: node, state: state, intervalMs: intervalMs, subscribed: subscribed)

func connName(status: ConnectionStatus): string =
  case status
  of ConnectionStatus.Connected: "connected"
  of ConnectionStatus.PartiallyConnected: "partially_connected"
  of ConnectionStatus.Disconnected: "disconnected"

proc beat(h: Heartbeat) {.async: (raises: [CancelledError]).} =
  let status =
    try:
      (await h.node.getConnectionStatus()).valueOr:
        ConnectionStatus.Disconnected
    except CancelledError as e:
      raise e
    except CatchableError:
      ConnectionStatus.Disconnected
  let c = h.state.counters
  h.state.enqueue(
    Record(
      kind: RecordKind.Heartbeat,
      atMs: nowMs(),
      conn: connName(status),
      subscribed: h.subscribed,
      droppedTotal: h.state.buffer.droppedTotal,
      missedTicksTotal: c.missedTicks,
      foreignTotal: c.foreign,
      unverifiedProbeTotal: c.unverifiedProbe,
      replayedTotal: c.replayed,
    )
  )

proc onStatusChange(h: Heartbeat) {.async: (raises: [CancelledError]).} =
  ## At most one extra heartbeat per interval; the periodic one covers the rest.
  let now = nowMs()
  if now - h.lastTriggeredMs < h.intervalMs:
    return
  h.lastTriggeredMs = now
  await h.beat()

proc listen*(h: Heartbeat): Result[void, string] =
  discard EventConnectionStatusChange.listen(
    h.node.waku.brokerCtx,
    proc(e: EventConnectionStatusChange) {.async: (raises: []).} =
      try:
        await h.onStatusChange()
      except CancelledError:
        discard,
  ).valueOr:
    return err("cannot listen for EventConnectionStatusChange: " & error)
  ok()

proc run*(h: Heartbeat) {.async: (raises: [CancelledError]).} =
  while not isRequested():
    await h.beat()
    await sleepAsync(milliseconds(h.intervalMs))
