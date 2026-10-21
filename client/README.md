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

For a client that keeps running after the SSH session closes and comes back after a reboot. The image downloads the release binary of the pinned version, verifies its checksum, and runs it as a non-root user from an otherwise empty image. It is `linux/amd64` only. On arm64 Docker Desktop runs it under emulation. On arm64 Linux, install a persistent registration (Debian/Ubuntu: `sudo apt install qemu-user-static binfmt-support`), or re-run `docker run --privileged --rm tonistiigi/binfmt --install amd64` after every reboot.

Needs Docker with Compose v2.

```
git clone https://github.com/logos-messaging/logos-delivery-loss-monitor
cd logos-delivery-loss-monitor/deploy/client
cp .env.example .env
```

Edit `.env` and set `LOSSMON_VERSION`, `LOSSMON_SERVER_URL` and `LOSSMON_LOCATION`. Then enter the token, which is read without echo so it stays out of shell history, and start the client. `chmod 644` lets the container user (uid 10001) read the token.

```
printf 'Token: '; stty -echo; IFS= read -r t; stty echo; echo; printf '%s\n' "$t" > token; unset t
chmod 644 token
docker compose up -d --build
```

`up -d` returns at once, so the terminal is free. The container restarts on failure and on boot (`restart: unless-stopped`) unless you stopped it.

| Task | Command (in `deploy/client`) |
|---|---|
| Follow the logs | `docker compose logs -f client` (Ctrl-C stops following, not the client) |
| Status | `docker compose ps` |
| Stop, keep state | `docker compose stop` |
| Start again | `docker compose start` |
| Stop and remove the container | `docker compose down` |
| Upgrade | set the new `LOSSMON_VERSION` in `.env`, then `docker compose up -d --build` |
| Check the version | `docker compose run --rm client --version` |

Settings come from `.env`: `LOSSMON_SERVER_URL`, `LOSSMON_LOCATION`, `LOSSMON_PRESET`, `LOSSMON_MODE`, `LOSSMON_ROLE` and `LOSSMON_RATE`. Any other option in [Run](#run) is passed the same way, as `LOSSMON_<NAME>` under `environment:` in `compose.yaml`. Changing `.env` takes effect with `docker compose up -d`.

The client id and node state live in the `lossmon-client_data` volume and survive restarts and upgrades. `docker compose down -v` deletes them, and the client registers again as a new client. On a shared host, keep the token private with `sudo chown 10001 token && chmod 400 token` instead of `chmod 644`.

Without Compose, from the repository root, with the token file in `deploy/client/token` as above:

```
docker build --platform linux/amd64 --build-arg LOSSMON_VERSION=0.2.0 -t lossmon-client:0.2.0 deploy/client
docker run -d --name lossmon-client --restart unless-stopped --platform linux/amd64 --read-only \
  --cap-drop ALL --security-opt no-new-privileges:true \
  --log-driver local --log-opt max-size=10m --log-opt max-file=5 \
  --mount type=bind,src="$PWD/deploy/client/token",dst=/run/token,readonly \
  -v lossmon-data:/data lossmon-client:0.2.0 \
  --server-url=https://lossmon.example.org --token-file=/run/token --location=eu
docker logs -f lossmon-client
```

## Build

Logos Delivery `v0.39.2` is a Nimble dependency (`lossmon_client.nimble`), locked in `nimble.lock` to the dependency set that release was tested with. There are no submodules.

```
make -C client lossmon-client     # build/lossmon-client
make -C client test               # unit tests
docker build --platform linux/amd64 --target prod client
```

`make` installs Nimble `0.26.0` (checksum-verified) under `~/.local`, fetches the dependencies into `nimbledeps/`, builds their C libraries, then builds `librln` from a pinned zerokit commit with `cargo` (a Rust toolchain is required). Set `LOG_LEVEL=INFO` to build with the node's info logging. On hosts where Nimble's prebuilt Nim does not match the CPU (arm64 Linux), point `LOSSMON_NIM` at a native Nim `2.2.6`. The image builds for `linux/amd64` only and fails fast on any other platform; pass `--platform linux/amd64` on arm64 hosts (see the emulation note above).

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
