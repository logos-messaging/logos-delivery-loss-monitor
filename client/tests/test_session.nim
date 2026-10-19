{.used.}

import chronos, chronos/apps/http/[httpclient, httpserver], results, unittest2
import ../src/[server_api, session]

const
  Token = "super-secret-token-value-0123456789abcdef"
  Port = 34782

var
  replies: seq[(HttpCode, string)]
  calls: int

proc handler(
    r: RequestFence
): Future[HttpResponseRef] {.async: (raises: [CancelledError]).} =
  if r.isErr():
    return defaultResponse()
  let request = r.get()
  {.gcsafe.}:
    inc calls
    let (status, body) =
      if replies.len > 0:
        replies.pop()
      else:
        (Http200, "{}")
    try:
      return await request.respond(status, body)
    except HttpError:
      return defaultResponse()

proc newSession(api: ServerApi): Session =
  result = initSession(
    api,
    SessionInfo(
      clientId: "c0000000-0000-4000-8000-000000000001",
      mode: "core",
      role: "both",
      location: "eu",
      preset: "logos.dev",
      ratePerTopic: 1.0,
      payloadBytes: 1024,
      anonymityLevel: "none",
    ),
    "50000000-0000-4000-8000-000000000001",
  )
  result.hasConfig = true

suite "session establish":
  let server = HttpServerRef.new(initTAddress("127.0.0.1:" & $Port), handler).get()
  server.start()
  let api = ServerApi.new("http://127.0.0.1:" & $Port, Token)

  setup:
    calls = 0

  test "mid-run, a clock_skew refusal is retried until it passes":
    replies = @[
      (Http200, "{}"),
      (Http422, """{"error":"clock_skew"}"""),
      (Http422, """{"error":"clock_skew"}"""),
    ]
    let res = waitFor newSession(api).establish(refetchConfig = false, startup = false)
    check res.isOk()
    check calls == 3

  test "at startup, a clock_skew refusal is fatal":
    replies = @[(Http422, """{"error":"clock_skew"}""")]
    let res = waitFor newSession(api).establish(refetchConfig = false, startup = true)
    check res.isErr()
    check calls == 1

  test "mid-run, a config mismatch is fatal":
    for code in ["topic_not_allowed", "rate_too_high", "network_mismatch"]:
      calls = 0
      replies = @[(Http422, "{\"error\":\"" & code & "\"}")]
      let res =
        waitFor newSession(api).establish(refetchConfig = false, startup = false)
      check res.isErr()
      check calls == 1

  waitFor api.close()
  waitFor server.stop()
  waitFor server.closeWait()
