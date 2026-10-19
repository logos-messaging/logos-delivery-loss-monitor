## Uploads buffered records to the server, one batch in flight at a time.

{.push raises: [].}

import std/[json, random]
import chronicles, chronos, chronos/apps/http/httpclient, results
import ./[backoff, buffer, clock, conf, records, server_api, session, shutdown, state]

logScope:
  topics = "lossmon reporter"

const
  UploadInterval = 1.seconds
  EagerBatchRecords = 500
  PollInterval = 100.milliseconds
  MaxBodyBytes = 900_000
  FlushTimeout* = 5.seconds

type
  BatchReaction* {.pure.} = enum
    Acked
    Reregister
    RetryAfter
    Split
    Drop
    Backoff

  Reporter* = ref object
    state: ClientState
    session: Session
    stopping: bool
    flushDeadline: Moment
    failures: int ## consecutive failed uploads

func reactToBatch*(status: int): BatchReaction =
  ## What a records reply asks the reporter to do next.
  case status
  of 200: BatchReaction.Acked
  of 404: BatchReaction.Reregister
  of 429: BatchReaction.RetryAfter
  of 413: BatchReaction.Split
  of 400, 422: BatchReaction.Drop
  else: BatchReaction.Backoff

func newReporter*(state: ClientState, session: Session): Reporter =
  Reporter(state: state, session: session)

proc stop*(r: Reporter) =
  ## Ends the upload loop after a last flush of up to `FlushTimeout`.
  r.flushDeadline = Moment.now() + FlushTimeout
  r.stopping = true

proc ackedSeq(body: string): Opt[int64] =
  try:
    let node = parseJson(body)
    return Opt.some(node["acked_seq"].getBiggestInt())
  except CatchableError:
    discard
  Opt.none(int64)

proc buildBatch(r: Reporter, maxRecords: int): tuple[count: int, body: string] =
  var count = min(maxRecords, r.state.buffer.len)
  while true:
    let batch = r.state.buffer.peek(count)
    let body = batchBody(nowMs(), batch)
    if body.len <= MaxBodyBytes or count <= 1:
      return (count, body)
    count = count div 2

proc uploadOnce(r: Reporter): Future[Duration] {.async: (raises: [CancelledError]).} =
  ## Sends one batch and returns how long to wait before the next attempt.
  let s = r.session
  let (count, body) = r.buildBatch(s.cfg.maxBatchRecords)
  if count == 0:
    return ZeroDuration
  let reply = (await s.api.call(MethodPost, "/v1/sessions/" & s.id & "/records", body)).valueOr:
    warn "upload failed", reason = error
    inc r.failures
    return backoffDelay(r.failures, rand(1.0))
  case reactToBatch(reply.status)
  of BatchReaction.Acked:
    let acked = ackedSeq(reply.body).valueOr:
      warn "the server sent no acked_seq"
      inc r.failures
      return backoffDelay(r.failures, rand(1.0))
    r.state.buffer.ack(acked)
    r.failures = 0
    ZeroDuration
  of BatchReaction.Reregister:
    notice "the server does not know this session; registering again"
    let res = await s.establish(refetchConfig = true, startup = false)
    if res.isErr() and not isRequested():
      error "re-registration failed", reason = res.error
      requestExit(ExitConfig)
    ZeroDuration
  of BatchReaction.RetryAfter:
    let hint = reply.retryAfterMs.get(reply.retryAfterBodyMs().get(1000))
    clampRetryAfter(hint)
  of BatchReaction.Split:
    if count <= 1:
      warn "the server refuses even a single record; dropping it"
      r.state.buffer.drop(1)
    else:
      r.session.cfg.maxBatchRecords = max(count div 2, 1)
    ZeroDuration
  of BatchReaction.Drop:
    error "the server rejected a batch; dropping it",
      status = reply.status, code = reply.errorCode()
    r.state.buffer.drop(count)
    ZeroDuration
  of BatchReaction.Backoff:
    warn "upload refused", status = reply.status, code = reply.errorCode()
    inc r.failures
    backoffDelay(r.failures, rand(1.0))

proc run*(r: Reporter) {.async: (raises: [CancelledError]).} =
  var
    lastUpload = Moment.now()
    notBefore = Moment.now()
  while true:
    let now = Moment.now()
    if r.stopping:
      if r.state.buffer.len == 0 or now >= r.flushDeadline:
        break
    let due =
      r.stopping or r.state.buffer.len >= EagerBatchRecords or
      now - lastUpload >= UploadInterval
    if due and now >= notBefore and r.state.buffer.len > 0:
      lastUpload = now
      let wait = await r.uploadOnce()
      notBefore = Moment.now() + wait
    elif r.state.buffer.len == 0:
      lastUpload = now
    await sleepAsync(PollInterval)
