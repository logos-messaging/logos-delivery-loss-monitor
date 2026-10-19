## HTTP client for the lossmon server. It follows no redirects, verifies TLS fully, and
## keeps the token and URL out of every error string it returns.

{.push raises: [].}

import std/[json, strutils]
import chronos, chronos/apps/http/httpclient, results, stew/byteutils

const RequestTimeout* = 20.seconds

type
  ApiReply* = object
    status*: int
    body*: string
    retryAfterMs*: Opt[int64] ## from the `Retry-After` header

  ServerApi* = ref object
    session: HttpSessionRef
    baseUrl: string
    authHeader: string

proc new*(T: type ServerApi, baseUrl, token: string): T =
  T(
    session: HttpSessionRef.new(maxRedirections = 0),
    baseUrl: baseUrl.strip(chars = {'/'}, leading = false),
    authHeader: "Bearer " & token,
  )

proc close*(api: ServerApi) {.async: (raises: []).} =
  await api.session.closeWait()

proc errorCode*(reply: ApiReply): string =
  ## The `error` field of a JSON error body, or empty.
  try:
    let node = parseJson(reply.body)
    if node.kind == JObject and node.hasKey("error"):
      return node["error"].getStr()
  except CatchableError:
    discard
  ""

proc retryAfterBodyMs*(reply: ApiReply): Opt[int64] =
  ## The `retry_after_ms` field of a JSON error body.
  try:
    let node = parseJson(reply.body)
    if node.kind == JObject and node.hasKey("retry_after_ms"):
      return Opt.some(node["retry_after_ms"].getBiggestInt())
  except CatchableError:
    discard
  Opt.none(int64)

proc call*(
    api: ServerApi, meth: HttpMethod, path: string, body = ""
): Future[Result[ApiReply, string]] {.async: (raises: []).} =
  ## Err carries a transport failure; an HTTP error status is a successful reply.
  var headers = @[("Authorization", api.authHeader), ("Accept", "application/json")]
  if body.len > 0:
    headers.add(("Content-Type", "application/json"))
  let request = HttpClientRequestRef.new(
    api.session, api.baseUrl & path, meth, headers = headers, body = body.toBytes()
  ).valueOr:
    return err("could not build the request: " & error)
  try:
    let response = await request.send().wait(RequestTimeout)
    try:
      let bytes = await response.getBodyBytes().wait(RequestTimeout)
      var reply = ApiReply(
        status: int(response.status),
        body: string.fromBytes(bytes),
        retryAfterMs: Opt.none(int64),
      )
      let retryAfter = response.headers.getString("retry-after")
      if retryAfter.len > 0:
        try:
          reply.retryAfterMs = Opt.some(parseBiggestInt(retryAfter) * 1000)
        except ValueError:
          discard
      if reply.status in 300 .. 399:
        return err("unexpected redirect (status " & $reply.status & ")")
      return ok(reply)
    finally:
      await response.closeWait()
  except AsyncTimeoutError:
    return err("request timed out")
  except CancelledError:
    return err("cancelled")
  except HttpError:
    return err("connection failed")
  finally:
    await request.closeWait()
