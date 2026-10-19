# Deploy

Compose stack for the lossmon server, Prometheus and Grafana. Images are pinned by digest. The server and (with the `listener` profile) the client are built from this repository.

## Run

```
git submodule update --init --recursive    # the client image builds from the vendored logos-delivery
cd deploy
cp .env.example .env                       # set GRAFANA_ADMIN_PASSWORD, preset and location
./init-secrets.sh                          # probe key, listener token, tokens file
docker compose up -d                       # server, Prometheus, Grafana
docker compose --profile listener up -d    # adds a core receiver on this host
```

Grafana is on `http://127.0.0.1:3000` (user `admin`), the server API on `http://127.0.0.1:8080`. Prometheus and the metrics port (9102) are not published.

The client image builds for linux/amd64 only.

## Profiles

| Profile | Adds |
|---|---|
| `listener` | `lossmon-client` in core mode, receive only, registered with the `listener` token |
| `public` | Caddy on 80/443 with ACME for `LOSSMON_PUBLIC_HOST`, proxying `/v1/*` only, no access log |

## Tokens

`secrets/tokens` holds one `<name> <scope> <token>` line per token (scope `report` or `query`, token at least 32 characters). After editing it, reload without a restart:

```
docker compose kill -s HUP server
```

Tokens, the probe key and `.env` are ignored by git. Rotating the probe key needs a server restart.

## Other clients

Clients on other hosts use the server's public URL and a token file:

```
docker run --rm -v "$PWD/token:/run/token:ro" -v lossmon-data:/data lossmon-client:local \
  --server-url=https://lossmon.example.org --token-file=/run/token --preset=logos.dev --location=eu
```
