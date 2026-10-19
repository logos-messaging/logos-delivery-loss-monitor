## Command line and `LOSSMON_*` environment configuration.

{.push raises: [].}

import std/[strutils, uri]
import results, confutils, confutils/defs

type
  ClientMode* {.pure.} = enum
    core
    edge

  ClientRole* {.pure.} = enum
    publish
    receive
    both

  AnonymityChoice* {.pure.} = enum
    none
    preferred
    required

const
  AppVersion* = "0.1.0"
  ExitConfig* = 78
  AllowedPresets* = ["logos.dev", "logos.test"]

type LossmonConf* = object
  serverUrl* {.desc: "Base URL of the lossmon server", name: "server-url".}: string

  token* {.defaultValue: "", desc: "Report token (prefer --token-file)", name: "token".}:
    string

  tokenFile* {.
    defaultValue: "", desc: "File holding the report token", name: "token-file"
  .}: string

  preset* {.
    defaultValue: "logos.dev",
    desc: "Network preset: logos.dev or logos.test",
    name: "preset"
  .}: string

  allowPreset* {.
    defaultValue: "",
    desc: "Preset outside the default allowlist that may be used",
    name: "allow-preset"
  .}: string

  mode* {.defaultValue: ClientMode.core, desc: "core or edge", name: "mode".}:
    ClientMode

  role* {.
    defaultValue: ClientRole.both, desc: "publish, receive or both", name: "role"
  .}: ClientRole

  topics* {.
    desc:
      "Content topic to use; repeat for several. Default: every topic the server lists",
    name: "topic"
  .}: seq[string]

  rate* {.defaultValue: 0.5, desc: "Messages per second per topic", name: "rate".}:
    float

  payloadBytes* {.
    defaultValue: 1024, desc: "Probe payload size in bytes", name: "payload-bytes"
  .}: int

  location* {.
    desc: "Coarse location label from the server allowlist", name: "location"
  .}: string

  anonymityLevel* {.
    defaultValue: AnonymityChoice.none,
    desc: "Sender anonymity level: none, preferred or required",
    name: "anonymity-level"
  .}: AnonymityChoice

  reliability* {.
    defaultValue: false,
    desc: "Enable store-based send reliability",
    name: "reliability"
  .}: bool

  dataDir* {.
    defaultValue: "/data",
    desc: "Directory for the client id and node state",
    name: "data-dir"
  .}: string

  bufferRecords* {.
    defaultValue: 100_000,
    desc: "Records held while the server is unreachable",
    name: "buffer-records"
  .}: int

  insecureHttp* {.
    defaultValue: false,
    desc: "Allow http:// to a non-loopback host",
    name: "insecure-http"
  .}: bool

  tcpPort* {.
    defaultValue: 0, desc: "TCP listen port; 0 keeps the node default", name: "tcp-port"
  .}: int

  discv5UdpPort* {.
    defaultValue: 0,
    desc: "discv5 UDP port; 0 keeps the node default",
    name: "discv5-udp-port"
  .}: int

  quicPort* {.
    defaultValue: 0, desc: "QUIC UDP port; 0 keeps the node default", name: "quic-port"
  .}: int

func canonicalPreset*(preset: string): string =
  ## Normalises a preset name the way the node's preset parser does.
  let lower = preset.toLowerAscii()
  case lower
  of "logosdev": "logos.dev"
  of "logostest": "logos.test"
  of "statusprod": "status.prod"
  else: lower

func checkPreset*(preset, allowPreset: string): Result[string, string] =
  ## Returns the canonical preset when this client may run on it.
  let canonical = canonicalPreset(preset)
  if canonical == "twn":
    return err("preset twn needs RLN credentials, which this client does not support")
  if canonical in AllowedPresets:
    return ok(canonical)
  if allowPreset.len > 0 and canonicalPreset(allowPreset) == canonical:
    return ok(canonical)
  err("preset " & canonical & " needs --allow-preset " & canonical)

func isLoopbackHost*(host: string): bool =
  let h = host.strip(chars = {'[', ']'}).toLowerAscii()
  h == "localhost" or h == "::1" or h == "127.0.0.1" or h.startsWith("127.")

func checkServerUrl*(url: string, insecureHttp: bool): Result[string, string] =
  ## Accepts https, or http to loopback; any other http needs `--insecure-http`.
  ## Returns the URL in normal form.
  var parsed: Uri
  try:
    parsed = parseUri(url)
  except ValueError:
    return err("--server-url is not a valid URL")
  if parsed.hostname.len == 0:
    return err("--server-url has no host")
  if parsed.username.len > 0 or parsed.password.len > 0:
    return err("--server-url must not carry credentials")
  case parsed.scheme
  of "https":
    ok($parsed)
  of "http":
    if isLoopbackHost(parsed.hostname) or insecureHttp:
      ok($parsed)
    else:
      err("http:// to a non-loopback host needs --insecure-http")
  else:
    err("--server-url must start with http:// or https://")
