# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

ARG BUN_VERSION=1.3.6
ARG GO_VERSION=1.27.1

FROM oven/bun:${BUN_VERSION}-slim@sha256:9d20d1b535596c4a021ba2087d2d303c4098e96be15f47775729cf7a259bb41e AS bun-base
WORKDIR /src

FROM bun-base AS web-dependencies
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --ignore-scripts --concurrent-scripts 2

FROM web-dependencies AS web-build
COPY web ./web
RUN bun run build:web

FROM golang:${GO_VERSION}-bookworm@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b AS go-toolchain
ENV CGO_ENABLED=0 GOTOOLCHAIN=local

FROM go-toolchain AS go-build
ARG VERSION=0.1.0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web-build /src/internal/dashboard/dist ./internal/dashboard/dist
RUN go build \
    -buildvcs=false \
    -trimpath \
    -tags production \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/codex-gateway \
    ./cmd/gateway

FROM bun-base AS development-runtime
ENV CGO_ENABLED=0 \
    DATA_DIR=/data/gateway \
    GOMAXPROCS=2 \
    GOTOOLCHAIN=local \
    HOME=/data/home \
    HOST=0.0.0.0 \
    LOG_LEVEL=debug \
    PATH=/usr/local/go/bin:${PATH} \
    PORT=8080 \
    VITE_HOST=0.0.0.0

USER root
RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates procps \
    && rm -rf /var/lib/apt/lists/*
COPY --from=go-toolchain /usr/local/go /usr/local/go
RUN mkdir -p /data/gateway /data/home

FROM development-runtime AS development
COPY package.json bun.lock go.mod go.sum ./
COPY --from=web-dependencies /src/node_modules ./node_modules
RUN go mod download
COPY . .

EXPOSE 5173 8080
VOLUME ["/data", "/src/node_modules"]

HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=5 \
  CMD ["bun", "-e", "const response = await fetch('http://127.0.0.1:5173/healthz'); process.exit(response.ok ? 0 : 1)"]

CMD ["bun", "run", "dev"]

FROM debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171 AS production
ENV DATA_DIR=/data/gateway \
    HOME=/data/home \
    HOST=0.0.0.0 \
    LOG_LEVEL=info \
    PORT=8080

RUN apt-get update \
    && apt-get install --yes --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 gateway \
    && useradd --uid 10001 --gid gateway --home-dir /data/home --no-create-home gateway \
    && mkdir -p /data/gateway /data/home

COPY --from=go-build /out/codex-gateway /usr/local/bin/codex-gateway

RUN chown -R gateway:gateway /data

USER gateway
EXPOSE 8080
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD ["curl", "--fail", "--silent", "http://127.0.0.1:8080/healthz"]

ENTRYPOINT ["/usr/local/bin/codex-gateway"]
