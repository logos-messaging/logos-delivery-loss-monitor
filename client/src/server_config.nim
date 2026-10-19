## The server's `GET /v1/config` document, with the client-side clamps applied.

{.push raises: [].}

import std/json
import results
import ./probe

const
  HeartbeatMinMs* = 1_000'i64
  HeartbeatMaxMs* = 60_000'i64
  BatchMin* = 1
  BatchMax* = 2000
  PayloadMin* = 64
  PayloadMax* = 65_536

type ServerConfig* = object
  probeKey*: ProbeKey
  probeKeyId*: string
  preset*: string
  clusterId*: int
  topics*: seq[string]
  locations*: seq[string]
  ephemeral*: bool
  maxRatePerClient*: float
  maxPayloadBytes*: int
  heartbeatIntervalMs*: int64
  maxBatchRecords*: int

func field(node: JsonNode, key: string, kind: JsonNodeKind): Result[JsonNode, string] =
  ## `std/json` getters return a default on a type mismatch, so check the kind first.
  let v = node.getOrDefault(key)
  if v.isNil():
    return err("the server config has no " & key)
  if v.kind != kind and not (kind == JFloat and v.kind == JInt):
    return err("the server config field " & key & " has the wrong type")
  ok(v)

func strField(node: JsonNode, key: string): Result[string, string] =
  ok((?node.field(key, JString)).getStr())

func intField(node: JsonNode, key: string): Result[int64, string] =
  ok((?node.field(key, JInt)).getBiggestInt())

func stringList(node: JsonNode, key: string): Result[seq[string], string] =
  var list: seq[string]
  for item in (?node.field(key, JArray)):
    if item.kind != JString:
      return err("the server config field " & key & " holds a non-string")
    list.add(item.getStr())
  ok(list)

proc parseServerConfig*(body: string): Result[ServerConfig, string] =
  var node: JsonNode
  try:
    node = parseJson(body)
  except CatchableError:
    return err("the server sent invalid JSON")
  if node.kind != JObject:
    return err("the server config is not a JSON object")
  var cfg: ServerConfig
  cfg.probeKey = ?parseProbeKey(?node.strField("probe_key"))
  cfg.probeKeyId = ?node.strField("probe_key_id")
  cfg.preset = ?node.strField("preset")
  cfg.clusterId = int(?node.intField("cluster_id"))
  cfg.topics = ?node.stringList("topics")
  cfg.locations = ?node.stringList("locations")
  cfg.ephemeral = (?node.field("ephemeral", JBool)).getBool()
  cfg.maxRatePerClient = (?node.field("max_rate_per_client", JFloat)).getFloat()
  cfg.maxPayloadBytes = int(?node.intField("max_payload_bytes"))
  cfg.heartbeatIntervalMs = ?node.intField("heartbeat_interval_ms")
  cfg.maxBatchRecords = int(?node.intField("max_batch_records"))
  if cfg.topics.len == 0:
    return err("the server config lists no topics")
  cfg.heartbeatIntervalMs =
    min(max(cfg.heartbeatIntervalMs, HeartbeatMinMs), HeartbeatMaxMs)
  cfg.maxBatchRecords = min(max(cfg.maxBatchRecords, BatchMin), BatchMax)
  cfg.maxPayloadBytes = min(max(cfg.maxPayloadBytes, PayloadMin), PayloadMax)
  ok(cfg)
