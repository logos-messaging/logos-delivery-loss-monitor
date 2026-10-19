# lossmon-client

Publishes probe messages on a Logos Delivery network and reports what it published and received to the lossmon server. It uses the Messaging API only (`EntryLayer.messaging`).

## Build

Logos Delivery `v0.39.2` is a Nimble dependency (`lossmon_client.nimble`), locked in `nimble.lock` to the dependency set that release was tested with. There are no submodules.

```
make -C client lossmon-client     # build/lossmon-client
make -C client test               # unit tests
docker build --platform linux/amd64 --target prod client
```

`make` installs Nimble `0.26.0` (checksum-verified) under `~/.local`, fetches the dependencies into `nimbledeps/`, builds their C libraries, then builds `librln` from a pinned zerokit commit with `cargo` (a Rust toolchain is required). Set `LOG_LEVEL=INFO` to build with the node's info logging. On hosts where Nimble's prebuilt Nim does not match the CPU (arm64 Linux), point `LOSSMON_NIM` at a native Nim `2.2.6`. The image builds for `linux/amd64` only; pass `--platform linux/amd64` on arm64 hosts.

To bump Logos Delivery, change the commit in `lossmon_client.nimble`, copy the revisions of the packages in that release's `nimble.lock` into the pins, run `nimble lock` and compare the result with the release's lock.

## Run

Options are `--name=value` (confutils does not take a space) or `LOSSMON_NAME` in the environment.

```
build/lossmon-client --server-url=https://lossmon.example.org --token-file=/run/token \
  --preset=logos.test --location=eu --mode=edge --role=both --rate=0.5
```

| Option | Default | |
|---|---|---|
| `--server-url` | required | https, or http to loopback; any other http needs `--insecure-http` |
| `--token-file` / `--token` | one required | the token file is preferred |
| `--location` | required | coarse label from the server allowlist |
| `--preset` | `logos.dev` | `logos.dev` or `logos.test`; others need `--allow-preset` |
| `--mode`, `--role` | `core`, `both` | |
| `--topic` | all the server's | repeatable |
| `--rate` | `0.5` | messages per second per topic |
| `--payload-bytes` | `1024` | at least 64 |
| `--data-dir` | `/data` | holds `client_id` and node state |

Exit code 78 means a configuration or registration problem the operator has to fix.
