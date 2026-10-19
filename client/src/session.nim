## Registration with the lossmon server: fetching its config, announcing this client's
## session, and recovering when the server rejects or forgets it.

{.push raises: [].}

import std/[json, random]
import chronicles, chronos, chronos/apps/http/httpclient, results
import ./[backoff, clock, conf, ids, keys, server_api, server_config, shutdown]

logScope:
  topics = "lossmon session"

type
  SessionInfo* = object
    ## Facts the server stores about the session; fixed for the life of the process.
    clientId*: string
    mode*: string
    role*: string
    location*: string
    preset*: string
    topics*: seq[string]
    ratePerTopic*: float
    payloadBytes*: int
    anonymityLevel*: string
    reliability*: bool
    logosDeliveryVersion*: string

  Session* = ref object
    api*: ServerApi
    info*: SessionInfo
    id*: string
    cfg*: ServerConfig
    keys*: ProbeKeys
    hasConfig*: bool

  Step {.pure.} = enum
    Done
    Retry
    Repeat
    Fatal

  StepResult = object
    step: Step
    wait: Duration
    detail: string
    midRunRetry: bool ## keep retrying after startup

func topics*(s: Session): seq[string] =
  ## The topics this session uses: the configured ones, else every topic the server lists.
  if s.info.topics.len > 0: s.info.topics else: s.cfg.topics

func initSession*(api: ServerApi, info: SessionInfo, sessionId: string): Session =
  Session(api: api, info: info, id: sessionId)

proc registerBody(s: Session): string =
  $(
    %*{
      "client_id": s.info.clientId,
      "mode": s.info.mode,
      "role": s.info.role,
      "location": s.info.location,
      "preset": s.info.preset,
      "cluster_id": s.cfg.clusterId,
      "topics": s.topics(),
      "rate_per_topic": s.info.ratePerTopic,
      "payload_bytes": s.info.payloadBytes,
      "ephemeral": s.cfg.ephemeral,
      "anonymity_level": s.info.anonymityLevel,
      "reliability": s.info.reliability,
      "app_version": AppVersion,
      "logos_delivery_version": s.info.logosDeliveryVersion,
      "probe_key_id": s.cfg.probeKeyId,
      "ntp_synced": false,
      "client_time_ms": nowMs(),
    }
  )

proc backoffFor(attempt: int): Duration =
  backoffDelay(attempt, rand(1.0))

proc fetchConfig(s: Session, attempt: int): Future[StepResult] {.async: (raises: []).} =
  let reply = (await s.api.call(MethodGet, "/v1/config")).valueOr:
    warn "cannot reach the server", reason = error
    return StepResult(step: Step.Retry, wait: backoffFor(attempt))
  case reply.status
  of 200:
    let cfg = parseServerConfig(reply.body).valueOr:
      return StepResult(step: Step.Fatal, detail: error)
    if canonicalPreset(cfg.preset) != canonicalPreset(s.info.preset):
      return StepResult(
        step: Step.Fatal,
        detail:
          "the server runs preset " & cfg.preset & ", this client " & s.info.preset,
      )
    s.cfg = cfg
    s.hasConfig = true
    s.keys.rotate(cfg.probeKey, nowMs(), cfg.receiptMaxAgeMs)
    StepResult(step: Step.Done)
  of 401, 403:
    StepResult(
      step: Step.Fatal,
      detail: "the server rejected the token (" & $reply.status & ")",
      midRunRetry: true,
    )
  of 429:
    StepResult(
      step: Step.Retry,
      wait: clampRetryAfter(reply.retryAfterMs.get(reply.retryAfterBodyMs().get(1000))),
    )
  else:
    warn "unexpected reply to the config request", status = reply.status
    StepResult(step: Step.Retry, wait: backoffFor(attempt))

proc putSession(s: Session, attempt: int): Future[StepResult] {.async: (raises: []).} =
  let reply = (await s.api.call(MethodPut, "/v1/sessions/" & s.id, s.registerBody())).valueOr:
    warn "cannot reach the server", reason = error
    return StepResult(step: Step.Retry, wait: backoffFor(attempt))
  let code = reply.errorCode()
  case reply.status
  of 200, 201:
    StepResult(step: Step.Done)
  of 404:
    StepResult(step: Step.Retry, wait: backoffFor(attempt))
  of 409:
    case code
    of "probe_key_changed":
      s.hasConfig = false
      StepResult(step: Step.Repeat)
    of "session_conflict":
      error "session_conflict: starting a new session"
      s.id = newUuid4().valueOr:
        return StepResult(step: Step.Fatal, detail: error)
      StepResult(step: Step.Repeat)
    of "client_id_conflict":
      StepResult(
        step: Step.Fatal,
        detail: "client_id is bound to another token; this data directory was copied",
      )
    else:
      StepResult(step: Step.Fatal, detail: "registration conflict: " & code)
  of 429:
    StepResult(
      step: Step.Retry,
      wait:
        clampRetryAfter(reply.retryAfterMs.get(reply.retryAfterBodyMs().get(60_000))),
    )
  of 401, 403:
    StepResult(
      step: Step.Fatal,
      detail: "the server rejected the token (" & $reply.status & ")",
      midRunRetry: true,
    )
  of 422:
    StepResult(
      step: Step.Fatal,
      detail: "registration refused: " & code,
      midRunRetry: code == "clock_skew",
    )
  else:
    warn "unexpected reply to registration", status = reply.status
    StepResult(step: Step.Retry, wait: backoffFor(attempt))

proc establish*(
    s: Session, refetchConfig: bool, startup: bool
): Future[Result[void, string]] {.async: (raises: [CancelledError]).} =
  ## Fetches the config when needed, then registers. Transient failures retry with
  ## backoff for as long as it takes. A fatal reply returns Err, except that
  ## `startup = false` keeps retrying after a 401, a 403 or a 422 `clock_skew`.
  var attempt = 0
  var needConfig = refetchConfig or not s.hasConfig
  while not isRequested():
    let res =
      if needConfig:
        await s.fetchConfig(attempt)
      else:
        await s.putSession(attempt)
    case res.step
    of Step.Done:
      if needConfig:
        needConfig = false
      else:
        return ok()
    of Step.Repeat:
      needConfig = not s.hasConfig
    of Step.Retry:
      inc attempt
      await sleepAsync(res.wait)
    of Step.Fatal:
      if startup or not res.midRunRetry:
        return err(res.detail)
      error "registration failed", reason = res.detail
      inc attempt
      await sleepAsync(backoffFor(attempt))
  return err("shutdown requested")
