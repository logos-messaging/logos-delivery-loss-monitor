{.used.}

import std/[os, osproc, strutils, uri, times]
import chronos, chronos/apps/http/httpclient, results, unittest2
import ../src/server_api

const Port = 34771

proc openssl(args: openArray[string]): bool =
  try:
    let p =
      startProcess("openssl", args = args, options = {poStdErrToStdOut, poUsePath})
    defer:
      p.close()
    p.waitForExit() == 0
  except CatchableError:
    false

proc attempt(flags: HttpClientFlags, url: string): Future[string] {.async.} =
  ## The server speaks TLS but no HTTP, so a client that accepts the certificate fails
  ## only when it reads the reply.
  let session = HttpSessionRef.new(flags = flags, maxRedirections = 0)
  try:
    discard await session.fetch(parseUri(url))
    result = "response"
  except HttpReadError:
    result = "handshake completed"
  except HttpError as e:
    result = e.msg
  finally:
    await session.closeWait()

suite "tls verification":
  test "a self-signed server is refused":
    if findExe("openssl").len == 0:
      skip()
    let dir = getTempDir() / ("lossmon-tls-" & $getTime().toUnix())
    createDir(dir)
    defer:
      removeDir(dir)
    check openssl(
      [
        "req",
        "-x509",
        "-newkey",
        "rsa:2048",
        "-nodes",
        "-keyout",
        dir / "key.pem",
        "-out",
        dir / "cert.pem",
        "-days",
        "1",
        "-subj",
        "/CN=localhost",
        "-addext",
        "subjectAltName=DNS:localhost",
      ]
    )
    let server = startProcess(
      "openssl",
      args = [
        "s_server",
        "-accept",
        $Port,
        "-cert",
        dir / "cert.pem",
        "-key",
        dir / "key.pem",
        "-www",
        "-tls1_2",
      ],
      options = {poStdErrToStdOut, poUsePath},
    )
    defer:
      server.terminate()
      discard server.waitForExit()
    sleep(1000)
    let url = "https://localhost:" & $Port & "/"
    check waitFor(attempt({}, url)).contains("X509NotTrusted")
    check waitFor(
      attempt({HttpClientFlag.NoVerifyHost, HttpClientFlag.NoVerifyServerName}, url)
    ) == "handshake completed"

    let api = ServerApi.new("https://localhost:" & $Port, "token")
    let reply = waitFor api.call(MethodGet, "/")
    waitFor api.close()
    check reply.isErr()
