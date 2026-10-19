# Deploy

Compose stack for the lossmon server, Prometheus and Grafana. Images are pinned by digest. The server and (with the `listener` profile) the client are built from this repository.

## Run

```
cd deploy
cp .env.example .env                       # set GRAFANA_ADMIN_PASSWORD, preset and location
./init-secrets.sh                          # probe key, listener token, tokens file (hashes only)
docker compose up -d                       # server, Prometheus, Grafana
docker compose --profile listener up -d    # adds a core receiver on this host
```

Grafana is on `http://127.0.0.1:${GRAFANA_PORT}` (default 3000, user `admin`), the server API on `http://127.0.0.1:${LOSSMON_SERVER_PORT}` (default 8080). Both stay on loopback; change the port in `.env`, never the `127.0.0.1:` prefix. Reach Grafana from a laptop with `ssh -L 3000:127.0.0.1:3000 <host>`.

Prometheus and the metrics port (9102) are not published, so the SSH tunnel does not reach Prometheus. Query it from Grafana Explore, or add `127.0.0.1:9090:9090` to the `prometheus` service.

The client image builds for linux/amd64 only.

## Host requirements

- Docker Engine 28 or newer. Before 28, a machine on the same L2 segment can reach ports published on `127.0.0.1` and unpublished container ports (Prometheus, the metrics port).
- Ports 80 and 443 must be free for the `public` profile: ACME needs both, and they cannot be moved. Check with `ss -ltn '( sport = :80 or sport = :443 )'`.
- Docker publishes ports ahead of ufw, so ufw rules do not restrict them. Everything except 80/443 is bound to loopback or unpublished. Restrict the source of 80/443 in the `DOCKER-USER` chain, and never publish another port without `127.0.0.1:`.

### Host already runs a web server

Leave the `public` profile off. On the existing proxy, add a virtual host for the public name that forwards `/v1/*` only to `127.0.0.1:${LOSSMON_SERVER_PORT}`, answers 404 for everything else, limits request bodies to 1 MiB and does not log client addresses. Do not publish port 8080 beyond loopback and do not use `--insecure-http`: both put bearer tokens on the wire in clear.

## Profiles

| Profile | Adds |
|---|---|
| `listener` | `lossmon-client` in core mode, receive only, registered with the `listener` token |
| `public` | Caddy on 80/443 with ACME for `LOSSMON_PUBLIC_HOST` (a hostname, no scheme or port), proxying `/v1/*` only, no access log and no error log lines for failed proxying |

## Tokens

`secrets/server/tokens` holds one `<name> <scope> <token>` line per token. The scope is `report` or `query`. The token is either a plain string of at least 32 characters or `sha256:<64 hex>`, the SHA-256 of the token. Keep only hashes on the deploy host: the server never needs the token itself.

```
./init-secrets.sh add-token alice report     # prints the token once, stores sha256:<hex>
docker compose kill -s HUP server            # reload without a restart
docker compose logs server | grep reloaded   # tokens reloaded count=2 names=alice,listener
```

Send each member their token over a private channel, one name per member. Give the `query` scope to the lead only: `GET /v1/stats?by=client` lists every token name and client id. The listener token is the only plaintext token on disk, in `secrets/listener_token`.

Tokens, the probe key and `.env` are ignored by git.

### Revoking a token

The server container mounts the directory `secrets/server`, not single files, so editors and `sed -i` (which write a new file) take effect on reload.

1. Edit `secrets/server/tokens` and delete the member's line.
2. `docker compose kill -s HUP server`.
3. Check that the `names=` list in the `tokens reloaded` log line no longer has the name. A failed reload logs `token reload failed` and keeps the previous set.
4. If the token leaked or the member left for good, also rotate the probe key. The key is handed to every client through `/v1/config`, so a revoked member can keep publishing valid probes until it changes. Write a new `secrets/server/probe_key` (`openssl rand -hex 32 > secrets/server/probe_key`, mode 644) and run `docker compose restart server`.

Restarting the server drops its in-memory run state, and the loss counters start again. Annotate the Grafana time range when you rotate. Running clients need no restart: their next report gets `unknown_session`, they log `the server does not know this session; registering again`, fetch `/v1/config` for the new key and carry on. A 409 `probe_key_changed` on registration makes a client fetch the config again the same way.

## Other clients

Clients on other hosts use the server's public URL and a token file:

```
docker run --rm -v "$PWD/token:/run/token:ro" -v lossmon-data:/data lossmon-client:local \
  --server-url=https://lossmon.example.org --token-file=/run/token --preset=logos.dev --location=eu
```
