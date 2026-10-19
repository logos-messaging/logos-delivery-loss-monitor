# lossmon-client

Publishes probe messages on a Logos Delivery network and reports what it published and received to the lossmon server. It uses the Messaging API only (`EntryLayer.messaging`).

## Build

The Logos Delivery tree is the submodule `vendor/logos-delivery`, pinned to `v0.39.2`.

```
git submodule update --init --recursive client/vendor/logos-delivery
make -C client lossmon-client     # build/lossmon-client
make -C client test               # unit tests
docker build --platform linux/amd64 --target prod --build-arg LD_VERSION=v0.39.2 client
```

`--recursive` is required: `librln` is built from the nested `zerokit` submodule. The image builds for `linux/amd64` only; pass `--platform linux/amd64` on arm64 hosts.

`make` builds the pinned Nim toolchain and dependencies on first use, then `librln` from the vendored zerokit (or downloads the prebuilt archive on glibc hosts). Set `LOG_LEVEL=INFO` to build with the node's info logging.

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
