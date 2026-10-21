FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8 AS fetch

ARG LOSSMON_VERSION=

RUN apk add --no-cache curl ca-certificates

WORKDIR /tmp/dl
RUN test "$(uname -m)" = x86_64 || { echo "linux/amd64 only; build with BuildKit and --platform linux/amd64" >&2; exit 1; } \
 && repo=https://github.com/logos-messaging/logos-delivery-loss-monitor \
 && v="${LOSSMON_VERSION#v}" \
 && if [ -z "$v" ]; then \
      loc=$(curl --proto '=https' --tlsv1.2 -fsS -o /dev/null -w '%{redirect_url}' "$repo/releases/latest") && v="${loc##*/v}"; \
    fi \
 && case "$v" in [0-9]*) ;; *) echo "cannot resolve the latest release; set LOSSMON_VERSION" >&2; exit 1 ;; esac \
 && f="lossmon-client_${v}_linux_amd64.tar.gz" \
 && base="$repo/releases/download/v${v}" \
 && curl --proto '=https' --tlsv1.2 -fsSL -O "${base}/${f}" -O "${base}/SHA256SUMS" \
 && grep " ${f}\$" SHA256SUMS | sha256sum -c - \
 && tar -xzf "${f}" \
 && install -m 755 "${f%.tar.gz}/lossmon-client" /lossmon-client \
 && mkdir /data

FROM scratch

LABEL source="https://github.com/logos-messaging/logos-delivery-loss-monitor"
LABEL description="lossmon client for Logos Delivery (release binary)"

COPY --from=fetch /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=fetch /lossmon-client /lossmon-client
COPY --from=fetch --chown=10001:10001 /data /data

USER 10001:10001
VOLUME /data
ENTRYPOINT ["/lossmon-client"]
