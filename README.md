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

## Binaries

Each [release](https://github.com/logos-messaging/logos-delivery-loss-monitor/releases) ships `lossmon-server` (Linux and macOS, amd64 and arm64) and `lossmon-client` (Linux amd64, macOS amd64 and arm64) with a `SHA256SUMS` file.

## License

Licensed under either of [Apache License, Version 2.0](LICENSE-APACHE) or [MIT license](LICENSE-MIT) at your option.
