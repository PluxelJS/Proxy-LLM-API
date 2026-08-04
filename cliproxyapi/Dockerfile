# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26
FROM golang:${GO_VERSION}-bookworm AS builder

ARG CLIPROXY_REPOSITORY=https://github.com/router-for-me/CLIProxyAPI.git
ARG CLIPROXY_REF=main

WORKDIR /src

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential git \
    && rm -rf /var/lib/apt/lists/* \
    && git init \
    && git remote add origin "${CLIPROXY_REPOSITORY}" \
    && git fetch --depth=1 origin "${CLIPROXY_REF}" \
    && git checkout --detach FETCH_HEAD

RUN go mod download

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=1 GOOS=linux go build \
    -buildvcs=false \
    -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.Commit=${COMMIT}' -X 'main.BuildDate=${BUILD_DATE}'" \
    -o /out/CLIProxyAPI ./cmd/server/

FROM debian:bookworm-slim

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="CLIProxyAPI for Proxy-LLM-API" \
      org.opencontainers.image.description="CLIProxyAPI built directly from router-for-me/CLIProxyAPI source" \
      org.opencontainers.image.source="https://github.com/PluxelJS/Proxy-LLM-API" \
      org.opencontainers.image.url="https://github.com/router-for-me/CLIProxyAPI" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.licenses="MIT"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /CLIProxyAPI/logs /CLIProxyAPI/plugins /data/auth

COPY --from=builder /out/CLIProxyAPI /CLIProxyAPI/CLIProxyAPI
COPY --from=builder /src/config.example.yaml /CLIProxyAPI/config.upstream.example.yaml

WORKDIR /CLIProxyAPI

ENV TZ=Asia/Taipei

EXPOSE 8317 1455 54545 51121

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl --fail --silent --show-error http://127.0.0.1:8317/healthz >/dev/null || exit 1

CMD ["./CLIProxyAPI", "-config", "/CLIProxyAPI/config.yaml"]
