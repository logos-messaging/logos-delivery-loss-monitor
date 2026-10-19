## lossmon-client: publishes and receives probe messages on a Logos Delivery network
## and reports what it saw to the lossmon server.

{.push raises: [].}

import std/[os, strutils]
import chronicles, chronos, confutils, results, system/ansi_c
import logos_delivery
import
  ./[
    clock, conf, heartbeat, ids, publisher, receiver, records, reporter, server_api,
    session, shutdown, state,
  ]

logScope:
  topics = "lossmon"

const
  ldVersion {.strdefine: "lossmon_ld_version".} = "unknown"
  SendQueueCapacity = 1000'u
  MaxParkedAgeSec = 1800'u
  ShutdownPoll = 100.milliseconds
  DrainTimeout = 5.seconds

proc fail(msg: string, code = ExitConfig) {.noreturn.} =
  error "cannot start", reason = msg
  quit(code)

proc readToken(c: LossmonConf): string =
  if (c.token.len > 0) == (c.tokenFile.len > 0):
    fail("give exactly one of --token and --token-file")
  if c.token.len > 0:
    warn "a token given directly is visible to other processes; prefer --token-file"
    return c.token
  try:
    result = readFile(c.tokenFile).strip()
  except CatchableError:
    fail("cannot read --token-file")
  if result.len == 0:
    fail("--token-file is empty")

func anonymity(c: AnonymityChoice): AnonymityLevel =
  case c
  of AnonymityChoice.none: AnonymityLevel.None
  of AnonymityChoice.preferred: AnonymityLevel.Preferred
  of AnonymityChoice.required: AnonymityLevel.Required

func nodeOverrides(c: LossmonConf): MessagingClientConf =
  result = MessagingClientConf(
    anonymityLevel: Opt.some(anonymity(c.anonymityLevel)),
    reliabilityEnabled: Opt.some(c.reliability),
    localStoragePath: Opt.some(c.dataDir),
    sendQueueCapacity: Opt.some(SendQueueCapacity),
    maxParkedAgeSec: Opt.some(MaxParkedAgeSec),
  )
  if c.tcpPort > 0:
    result.p2pTcpPort = Opt.some(Port(c.tcpPort))
  if c.discv5UdpPort > 0:
    result.discv5UdpPort = Opt.some(Port(c.discv5UdpPort))
  if c.quicPort > 0:
    result.quicPort = Opt.some(Port(c.quicPort))

proc guarded[T](
    f: Future[Result[T, string]]
): Future[Result[T, string]] {.async: (raises: [CancelledError]).} =
  try:
    return await f
  except CancelledError as e:
    raise e
  except CatchableError as e:
    return err(e.msg)

func publishes(c: LossmonConf): bool =
  c.role in {ClientRole.publish, ClientRole.both}

func receives(c: LossmonConf): bool =
  c.role in {ClientRole.receive, ClientRole.both}

proc run(c: LossmonConf) {.async: (raises: [CancelledError]).} =
  let preset = checkPreset(c.preset, c.allowPreset).valueOr:
    fail(error)
  let url = checkServerUrl(c.serverUrl, c.insecureHttp).valueOr:
    fail(error)
  let token = readToken(c)
  if c.rate <= 0.0:
    fail("--rate must be positive")
  let clientId = loadOrCreateClientId(c.dataDir).valueOr:
    fail(error)
  let sessionId = newUuid4().valueOr:
    fail(error)

  let api = ServerApi.new(url, token)
  let session = initSession(
    api,
    SessionInfo(
      clientId: clientId,
      mode: $c.mode,
      role: $c.role,
      location: c.location,
      preset: preset,
      topics: c.topics,
      ratePerTopic: c.rate,
      payloadBytes: c.payloadBytes,
      anonymityLevel: $c.anonymityLevel,
      reliability: c.reliability,
      logosDeliveryVersion: ldVersion,
    ),
    sessionId,
  )
  (await session.establish(refetchConfig = true, startup = true)).isOkOr:
    if isRequested():
      quit(exitCode())
    fail(error)
  if c.location notin session.cfg.locations:
    warn "location is not on the server allowlist and will be reported as other",
      location = c.location
  let topics = session.topics()
  notice "registered", session = sessionId, topics = topics.len, role = $c.role

  let node = (
    await guarded(
      LogosDelivery.new(
        entryLayer = EntryLayer.messaging,
        mode =
          if c.mode == ClientMode.core:
            LogosDeliveryMode.Core
          else:
            LogosDeliveryMode.Edge,
        preset = preset,
        messagingOverrides = nodeOverrides(c),
      )
    )
  ).valueOr:
    fail("cannot create the node: " & error, 1)
  (await guarded(node.start())).isOkOr:
    fail("cannot start the node: " & error, 1)

  let state = newClientState(c.bufferRecords)
  var tasks: seq[Future[void].Raising([CancelledError])]
  var pub: Publisher

  var subscribed: seq[string]
  var rx: Receiver
  if c.receives():
    rx = newReceiver(state, session, topics)
    rx.listen(node.messagingClient).isOkOr:
      fail(error, 1)
    for topic in topics:
      (await guarded(node.messagingClient.subscribe(topic))).isOkOr:
        fail("cannot subscribe to " & topic & ": " & error, 1)
    subscribed = topics

  let hb = newHeartbeat(node, state, session.cfg.heartbeatIntervalMs, subscribed)
  hb.listen().isOkOr:
    fail(error, 1)
  tasks.add(hb.run())

  if c.publishes():
    let hook: OwnProbeHook =
      if c.receives():
        proc(probeId: string, at: int64) {.gcsafe, raises: [].} =
          rx.markOwn(probeId, at)
      else:
        proc(probeId: string, at: int64) {.gcsafe, raises: [].} =
          discard
    pub = newPublisher(
      node.messagingClient,
      state,
      session,
      topics,
      c.rate,
      c.payloadBytes,
      int(SendQueueCapacity),
      int64(MaxParkedAgeSec),
      hook,
    )
    pub.listen().isOkOr:
      fail(error, 1)
    tasks.add(pub.run())

  let rep = newReporter(state, session)
  let repLoop = rep.run()

  while not isRequested():
    await sleepAsync(ShutdownPoll)
  notice "shutting down"

  for t in tasks:
    await cancelAndWait(t)
  if not pub.isNil():
    let deadline = Moment.now() + DrainTimeout
    while pub.hasPending() and Moment.now() < deadline:
      await sleepAsync(ShutdownPoll)
  if exitCode() == 0:
    state.enqueue(Record(kind: RecordKind.End, atMs: nowMs()))
  state.close()
  rep.stop()
  await repLoop
  await api.close()
  quit(exitCode())

proc main() =
  var c: LossmonConf
  try:
    c = LossmonConf.load(
      version = "lossmon-client " & AppVersion, envVarsPrefix = "LOSSMON"
    )
  except CatchableError as e:
    fail("bad configuration: " & e.msg)
  setControlCHook(
    proc() {.noconv.} =
      requestShutdown(ansi_c.SIGINT)
  )
  when defined(posix):
    c_signal(ansi_c.SIGTERM, requestShutdown)
  try:
    waitFor run(c)
  except CatchableError as e:
    fail("stopped: " & e.msg, 1)

when isMainModule:
  main()
