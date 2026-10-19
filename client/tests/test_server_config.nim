{.used.}

import std/[json, strutils]
import results, unittest2
import ../src/[ids, server_config]

func body(overrides: JsonNode = newJObject()): string =
  var j = %*{
    "probe_key": "0x" & "ab".repeat(32),
    "probe_key_id": "e543f63c",
    "preset": "logos.test",
    "cluster_id": 2,
    "topics": ["/lossmon/1/probe-0/raw"],
    "locations": ["eu", "na"],
    "ephemeral": false,
    "max_rate_per_client": 1.0,
    "max_payload_bytes": 4096,
    "heartbeat_interval_ms": 10000,
    "max_batch_records": 2000,
    "receipt_max_age_ms": 1_230_000,
    "server_time_ms": 1760870000123,
  }
  for k, v in overrides:
    j[k] = v
  $j

suite "server config":
  test "a well-formed document parses":
    let cfg = parseServerConfig(body()).get()
    check cfg.preset == "logos.test"
    check cfg.clusterId == 2
    check cfg.topics == @["/lossmon/1/probe-0/raw"]
    check cfg.heartbeatIntervalMs == 10_000
    check cfg.maxBatchRecords == 2000
    check cfg.receiptMaxAgeMs == 1_230_000
    check cfg.probeKeyId == "e543f63c"

  test "values are clamped to the client limits":
    let low = parseServerConfig(
        body(
          %*{
            "heartbeat_interval_ms": 5,
            "max_batch_records": 0,
            "max_payload_bytes": 1,
            "receipt_max_age_ms": 0,
          }
        )
      )
      .get()
    check low.heartbeatIntervalMs == 1000
    check low.maxBatchRecords == 1
    check low.maxPayloadBytes == 64
    check low.receiptMaxAgeMs == ReceiptMaxAgeMin
    let high = parseServerConfig(
        body(
          %*{
            "heartbeat_interval_ms": 9_999_999,
            "max_batch_records": 99_999,
            "max_payload_bytes": 99_999_999,
            "receipt_max_age_ms": 9_999_999_999_999,
          }
        )
      )
      .get()
    check high.heartbeatIntervalMs == 60_000
    check high.maxBatchRecords == 2000
    check high.maxPayloadBytes == 65_536
    check high.receiptMaxAgeMs == ReceiptMaxAgeMax

  test "a server without receipt_max_age_ms gets the default bound":
    var j = parseJson(body())
    j.delete("receipt_max_age_ms")
    check parseServerConfig($j).get().receiptMaxAgeMs == ReceiptMaxAgeDefault

  test "a bad probe key is refused":
    check parseServerConfig(body(%*{"probe_key": "0x1234"})).isErr()
    check parseServerConfig(body(%*{"probe_key": "nothex"})).isErr()

  test "missing fields and wrong types are refused":
    check parseServerConfig("{}").isErr()
    check parseServerConfig("not json").isErr()
    check parseServerConfig("[]").isErr()
    check parseServerConfig(body(%*{"cluster_id": "two"})).isErr()
    check parseServerConfig(body(%*{"receipt_max_age_ms": "soon"})).isErr()

  test "an empty topic list is refused":
    check parseServerConfig(body(%*{"topics": []})).isErr()

suite "identifiers":
  test "generated uuids are lowercase v4":
    for _ in 0 ..< 50:
      let id = newUuid4().get()
      check isUuid(id)
      check id[14] == '4'
      check id[19] in {'8', '9', 'a', 'b'}

  test "uuid validation":
    check not isUuid("")
    check not isUuid("A8674BD9-CC75-44CA-A06E-A33964AABA66")
    check not isUuid("a8674bd9cc7544caa06ea33964aaba66")
    check isUuid("a8674bd9-cc75-44ca-a06e-a33964aaba66")
