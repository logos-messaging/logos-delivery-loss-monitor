FROM golang:1.26.10-alpine@sha256:9ce231b28998b32e116927c61aa5c406183061d4f3f2d61b292c88e7f6bdf7ca AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/lossmon-server ./cmd/lossmon-server

FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8

LABEL source="https://github.com/logos-messaging/logos-delivery-loss-monitor"
LABEL description="lossmon server for Logos Delivery"

RUN adduser -D -u 10001 lossmon
COPY --from=build /out/lossmon-server /usr/local/bin/lossmon-server

USER lossmon
EXPOSE 8080 9102
ENTRYPOINT ["/usr/local/bin/lossmon-server"]
