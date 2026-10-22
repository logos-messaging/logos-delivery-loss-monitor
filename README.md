# logos-delivery-loss-monitor

Measures message loss in a Logos Delivery network. Clients publish and receive messages on pre-defined content topics through the Messaging API and report the message IDs to a server, which pairs publishes with receipts and exposes the loss ratio as Prometheus metrics.

## Quick start

Needs Docker with Compose v2. The client image builds for `linux/amd64` only; on an arm64 host Docker builds it under emulation (the listener service already sets `platform: linux/amd64`), which is slow.

```
git clone https://github.com/logos-messaging/logos-delivery-loss-monitor
cd logos-delivery-loss-monitor
cd deploy
cp .env.example .env                       # set GRAFANA_ADMIN_PASSWORD, preset and location
./init-secrets.sh
docker compose --profile listener up -d --build
```

Grafana is on `http://127.0.0.1:3000`; see [`deploy/README.md`](deploy/README.md) for profiles, tokens and other clients.

## Install

Linux amd64, macOS amd64 and arm64. Installs to `~/.local/bin` without sudo; set `LOSSMON_VERSION` or `LOSSMON_INSTALL_DIR` to change the version or directory.

```
curl --proto '=https' --tlsv1.2 -fsSL https://github.com/logos-messaging/logos-delivery-loss-monitor/releases/latest/download/install.sh | sh
```

With a pinned version, set the variables on `sh`:

```
curl --proto '=https' --tlsv1.2 -fsSL https://github.com/logos-messaging/logos-delivery-loss-monitor/releases/latest/download/install.sh | LOSSMON_VERSION=0.3.0 sh
```

To read the script before running it:

```
curl --proto '=https' --tlsv1.2 -fsSLO https://github.com/logos-messaging/logos-delivery-loss-monitor/releases/latest/download/install.sh
less install.sh
sh install.sh
```

Manually, with the checksum verified (macOS: `OS=darwin`, `ARCH=arm64` or `amd64`, and `shasum -a 256` instead of `sha256sum`):

```
V=0.3.0; OS=linux; ARCH=amd64
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

To keep a client running after you log out of SSH, see [Run detached with Docker](client/README.md#run-detached-with-docker).

## Binaries

Each [release](https://github.com/logos-messaging/logos-delivery-loss-monitor/releases) ships `lossmon-server` (Linux and macOS, amd64 and arm64) and `lossmon-client` (Linux amd64, macOS amd64 and arm64) with a `SHA256SUMS` file.

## License

Licensed under either of [Apache License, Version 2.0](LICENSE-APACHE) or [MIT license](LICENSE-MIT) at your option.
