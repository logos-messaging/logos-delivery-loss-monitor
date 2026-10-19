FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8 AS build

ARG LD_VERSION=unknown
ARG LOG_LEVEL=NOTICE

RUN apk add --no-cache bash git build-base cmake curl jq linux-headers openssl-dev libbsd-dev openssl rust cargo

WORKDIR /app
COPY . .

RUN make -C vendor/logos-delivery -j"$(nproc)" build-deps

RUN cargo build --release -p rln \
      --manifest-path vendor/logos-delivery/vendor/zerokit/rln/Cargo.toml \
      --no-default-features --features stateless \
 && cp vendor/logos-delivery/vendor/zerokit/target/release/librln.a \
       vendor/logos-delivery/librln_v2.0.2.a

FROM build AS test
RUN make test LD_VERSION="${LD_VERSION}" LOG_LEVEL="${LOG_LEVEL}"

FROM build AS binary
RUN make lossmon-client LD_VERSION="${LD_VERSION}" LOG_LEVEL="${LOG_LEVEL}"

FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8 AS prod

LABEL source="https://github.com/logos-messaging/logos-delivery-loss-monitor"
LABEL description="lossmon client for Logos Delivery"

RUN apk add --no-cache libgcc libstdc++ libgomp \
 && adduser -D -u 10001 lossmon \
 && mkdir /data \
 && chown lossmon /data

COPY --from=binary /app/build/lossmon-client /usr/local/bin/lossmon-client

USER lossmon
VOLUME /data
ENTRYPOINT ["/usr/local/bin/lossmon-client"]
