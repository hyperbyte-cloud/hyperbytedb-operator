FROM golang:1.25-bookworm AS builder

WORKDIR /src
COPY main.go .
RUN go mod init hyperbytedb-e2e-stub \
    && CGO_ENABLED=0 go build -o hyperbytedb .

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/hyperbytedb /usr/local/bin/hyperbytedb

EXPOSE 8086
