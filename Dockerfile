FROM golang:1.25-bookworm AS builder

WORKDIR /build

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc libc-dev sqlite3 libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 go build -ldflags="-s -w -X main.version=$(git describe --tags --always 2>/dev/null || echo dev)" \
    -o /viking-go ./cmd/viking-go

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates sqlite3 libsqlite3-0 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -r viking && useradd -r -g viking -m viking

COPY --from=builder /viking-go /usr/local/bin/viking-go

RUN mkdir -p /data && chown viking:viking /data

USER viking
WORKDIR /data

EXPOSE 6920

ENV VIKING_DATA_DIR=/data

ENTRYPOINT ["viking-go"]
CMD ["--host", "0.0.0.0", "--port", "6920"]
