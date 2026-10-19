FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8 AS build

ARG LOG_LEVEL=NOTICE

RUN apk add --no-cache bash git build-base cmake curl linux-headers openssl-dev libbsd-dev openssl rust cargo

WORKDIR /app
COPY . .

RUN make deps

FROM build AS test
RUN make test LOG_LEVEL="${LOG_LEVEL}"

FROM build AS binary
RUN make lossmon-client LOG_LEVEL="${LOG_LEVEL}"

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
