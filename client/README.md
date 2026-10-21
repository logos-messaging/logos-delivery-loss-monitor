# lossmon-client

Publishes probe messages on a Logos Delivery network and reports what it published and received to the lossmon server. It uses the Messaging API only (`EntryLayer.messaging`).

## Install

Linux amd64, macOS amd64 and arm64. Installs to `~/.local/bin` without sudo; set `LOSSMON_VERSION` or `LOSSMON_INSTALL_DIR` to change the version or directory.

```
curl --proto '=https' --tlsv1.2 -fsSL https://github.com/logos-messaging/logos-delivery-loss-monitor/releases/latest/download/install.sh | sh
```

With a pinned version, set the variables on `sh`:

```
curl --proto '=https' --tlsv1.2 -fsSL https://github.com/logos-messaging/logos-delivery-loss-monitor/releases/latest/download/install.sh | LOSSMON_VERSION=0.2.0 sh
```

To read the script before running it:

```
curl --proto '=https' --tlsv1.2 -fsSLO https://github.com/logos-messaging/logos-delivery-loss-monitor/releases/latest/download/install.sh
less install.sh
sh install.sh
```

Manually, with the checksum verified (macOS: `OS=darwin`, `ARCH=arm64` or `amd64`, and `shasum -a 256` instead of `sha256sum`):

```
V=0.2.0; OS=linux; ARCH=amd64
f=lossmon-client_${V}_${OS}_${ARCH}.tar.gz
base=https://github.com/logos-messaging/logos-delivery-loss-monitor/releases/download/v$V
curl --proto '=https' --tlsv1.2 -fL -O "$base/$f" -O "$base/SHA256SUMS"
sha256sum -c SHA256SUMS --ignore-missing
tar -xzf "$f" && mkdir -p ~/.local/bin && install -m 755 "${f%.tar.gz}/lossmon-client" ~/.local/bin/
lossmon-client --version
```

The client writes to `/data` by default, which a normal user cannot create. Pass a per-user directory:

```
lossmon-client --data-dir="$HOME/.local/share/lossmon" --server-url=... --token-file=... --location=...
```

A binary downloaded in a browser is quarantined by macOS; clear it with `xattr -d com.apple.quarantine lossmon-client`. Downloads through `curl` are not affected.

## Run detached with Docker

For a client that keeps running after the SSH session closes and comes back after a reboot. The image downloads the latest release binary, verifies its checksum and runs it as a non-root user. It is `linux/amd64` only; on arm64 Docker runs it under emulation. Needs Docker with Compose v2.

```
git clone --depth 1 --branch v<version> https://github.com/logos-messaging/logos-delivery-loss-monitor
cd logos-delivery-loss-monitor/deploy/client
cp .env.example .env
```

Replace `<version>` with the release you are installing (v0.3.0 or later, the first with `deploy/client`). Edit `.env` and set `LOSSMON_SERVER_URL`, `LOSSMON_LOCATION` and `LOSSMON_TOKEN`. The token is the server's `report` token from the server admin (see [Tokens](../deploy/README.md#tokens)). For a preset other than `logos.dev` and `logos.test`, also set `LOSSMON_ALLOW_PRESET` to that preset. Then start the client:

```
docker compose up -d --build
```

The client logs a warning that the token is passed directly; that is expected here.

Docker caches the download, so a rebuild needs `--no-cache` to pick up a newer release. To pin a release, set `LOSSMON_VERSION` (for example `0.3.0`) in `.env`.

| Task | Command (in `deploy/client`) |
|---|---|
| Follow the logs | `docker compose logs -f client` |
| Stop / start | `docker compose stop` / `docker compose start` |
| Remove the container | `docker compose down` |
| Change a setting | edit `.env`, then `docker compose up -d` |
| Change the token | `docker compose down -v`, edit `.env`, then `docker compose up -d` (a client id is bound to its token) |
| Upgrade | `git fetch --depth 1 origin tag v<new> && git checkout v<new>`, then `docker compose build --no-cache && docker compose up -d` |

The client id and node state live in the `lossmon-client_data` volume and survive restarts and upgrades. `docker compose down -v` deletes them, and the client then registers as a new client.

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
