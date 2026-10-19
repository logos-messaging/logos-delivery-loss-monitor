{.used.}

import std/strutils
import stew/byteutils
import chronos, chronos/apps/http/[httpclient, httpserver], results, unittest2
import ../src/server_api

const
  Token = "super-secret-token-value-0123456789abcdef"
  Port = 34781

type Reply = object
  status: HttpCode
  body: string
  headers: seq[(string, string)]

var
  next: Reply
  seenAuth: string
  seenPath: string
  seenBody: string

proc handler(
    r: RequestFence
): Future[HttpResponseRef] {.async: (raises: [CancelledError]).} =
  if r.isErr():
    return defaultResponse()
  let request = r.get()
  {.gcsafe.}:
    seenAuth = request.headers.getString("authorization")
    seenPath = request.uri.path
    try:
      seenBody = string.fromBytes(await request.getBody())
      return await request.respond(next.status, next.body, HttpTable.init(next.headers))
    except HttpError:
      return defaultResponse()

proc serve(): HttpServerRef =
  let address = initTAddress("127.0.0.1:" & $Port)
  HttpServerRef.new(address, handler).get()

suite "server api":
  let server = serve()
  server.start()
  let api = ServerApi.new("http://127.0.0.1:" & $Port, Token)

  teardown:
    next = Reply()

  test "a successful call returns status and body":
    next = Reply(status: Http200, body: """{"ok":true}""")
    let reply = waitFor(api.call(MethodGet, "/v1/config")).get()
    check reply.status == 200
    check reply.body == """{"ok":true}"""
    check seenPath == "/v1/config"

  test "the bearer token is sent":
    next = Reply(status: Http200, body: "{}")
    discard waitFor api.call(MethodGet, "/v1/config")
    check seenAuth == "Bearer " & Token

  test "a body is sent with a post":
    next = Reply(status: Http200, body: "{}")
    discard waitFor api.call(MethodPost, "/v1/sessions/x/records", """{"a":1}""")
    check seenBody == """{"a":1}"""

  test "an error status is a reply, not a transport error":
    next = Reply(status: Http409, body: """{"error":"probe_key_changed"}""")
    let reply = waitFor(api.call(MethodPut, "/v1/sessions/x", "{}")).get()
    check reply.status == 409
    check reply.errorCode() == "probe_key_changed"

  test "the retry-after header and body hint are read":
    next = Reply(
      status: Http429,
      body: """{"error":"rate_limited","retry_after_ms":1500}""",
      headers: @[("Retry-After", "2")],
    )
    let reply = waitFor(api.call(MethodGet, "/v1/config")).get()
    check reply.retryAfterMs == Opt.some(2000'i64)
    check reply.retryAfterBodyMs() == Opt.some(1500'i64)

  test "an enormous or negative retry-after header is clamped":
    for value in ["9223372036854775807", "-5"]:
      next = Reply(status: Http429, body: "{}", headers: @[("Retry-After", value)])
      let reply = waitFor(api.call(MethodGet, "/v1/config")).get()
      check reply.retryAfterMs.get() in 0'i64 .. 86_400_000'i64

  test "an unparsable error body gives no code":
    next = Reply(status: Http500, body: "<html>")
    let reply = waitFor(api.call(MethodGet, "/v1/config")).get()
    check reply.errorCode() == ""
    check reply.retryAfterBodyMs().isNone()

  test "a redirect is an error and is not followed":
    next =
      Reply(status: Http302, body: "", headers: @[("Location", "http://127.0.0.1:1/")])
    let reply = waitFor api.call(MethodGet, "/v1/config")
    check reply.isErr()
    check "redirect" in reply.error

  test "an unreachable server is a transport error without the token":
    let dead = ServerApi.new("http://127.0.0.1:1", Token)
    let reply = waitFor dead.call(MethodGet, "/v1/config")
    waitFor dead.close()
    check reply.isErr()
    check Token notin reply.error

  waitFor api.close()
  waitFor server.stop()
  waitFor server.closeWait()
