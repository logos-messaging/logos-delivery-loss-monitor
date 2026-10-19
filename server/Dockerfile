FROM golang:1.26.9-alpine@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade AS build

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
