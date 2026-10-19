# logos-delivery-loss-monitor

Measures message loss in a Logos Delivery network. Clients publish and receive messages on pre-defined content topics through the Messaging API and report the message IDs to a server, which pairs publishes with receipts and exposes the loss ratio as Prometheus metrics.

## Quick start

Needs Docker with Compose v2. The client image builds for `linux/amd64` only; on an arm64 host Docker builds it under emulation (the listener service already sets `platform: linux/amd64`), which is slow.

```
git clone --recurse-submodules https://github.com/logos-messaging/logos-delivery-loss-monitor
cd logos-delivery-loss-monitor
git submodule update --init --recursive    # needed if you cloned without --recurse-submodules
cd deploy
cp .env.example .env                       # set GRAFANA_ADMIN_PASSWORD, preset and location
./init-secrets.sh
docker compose --profile listener up -d --build
```

`--recursive` matters: `librln` is built from `zerokit`, a submodule of the vendored `logos-delivery`, and a one-level init leaves it empty. Grafana is on `http://127.0.0.1:3000`; see [`deploy/README.md`](deploy/README.md) for profiles, tokens and other clients.

## License

Licensed under either of [Apache License, Version 2.0](LICENSE-APACHE) or [MIT license](LICENSE-MIT) at your option.
