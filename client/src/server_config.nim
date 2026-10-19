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
  ReceiptMaxAgeMin* = 60_000'i64
  ReceiptMaxAgeMax* = 24 * 3_600_000'i64
  ReceiptMaxAgeDefault* = 600_000'i64 ## for servers that do not publish the bound
  ClockOffsetMax* = 600_000'i64
  ClockOffsetDefault* = 60_000'i64 ## for servers that do not publish the bound

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
  receiptMaxAgeMs*: int64
  maxClockOffsetMs*: int64

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
  cfg.receiptMaxAgeMs = ReceiptMaxAgeDefault
  if node.hasKey("receipt_max_age_ms"):
    cfg.receiptMaxAgeMs = ?node.intField("receipt_max_age_ms")
  cfg.maxClockOffsetMs = ClockOffsetDefault
  if node.hasKey("max_clock_offset_ms"):
    cfg.maxClockOffsetMs = ?node.intField("max_clock_offset_ms")
  if cfg.topics.len == 0:
    return err("the server config lists no topics")
  cfg.heartbeatIntervalMs =
    min(max(cfg.heartbeatIntervalMs, HeartbeatMinMs), HeartbeatMaxMs)
  cfg.maxBatchRecords = min(max(cfg.maxBatchRecords, BatchMin), BatchMax)
  cfg.maxPayloadBytes = min(max(cfg.maxPayloadBytes, PayloadMin), PayloadMax)
  cfg.receiptMaxAgeMs =
    min(max(cfg.receiptMaxAgeMs, ReceiptMaxAgeMin), ReceiptMaxAgeMax)
  cfg.maxClockOffsetMs = min(max(cfg.maxClockOffsetMs, 0), ClockOffsetMax)
  ok(cfg)

func receiptFilterMs*(cfg: ServerConfig): int64 =
  ## The oldest receipt, by message timestamp, worth reporting. The server measures age on
  ## the corrected clock, so the bound leaves room for this client's clock offset.
  max(cfg.receiptMaxAgeMs - cfg.maxClockOffsetMs, cfg.receiptMaxAgeMs div 2)
