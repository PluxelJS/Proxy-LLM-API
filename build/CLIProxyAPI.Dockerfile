# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26
FROM golang:${GO_VERSION}-bookworm AS builder

ARG CLIPROXY_REF=main

WORKDIR /src

RUN apt-get update \
    && apt-get install -y --no-install-recommends build-essential git \
    && rm -rf /var/lib/apt/lists/* \
    && git init \
    && git remote add origin https://github.com/router-for-me/CLIProxyAPI.git \
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

EXPOSE 8317

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD curl --fail --silent --show-error http://127.0.0.1:8317/healthz >/dev/null || exit 1

CMD ["./CLIProxyAPI", "-config", "/CLIProxyAPI/config.yaml"]

# Ship the official panel with the image so first access never needs a download.
ARG MANAGEMENT_UI_VERSION=v1.22.15
ARG MANAGEMENT_UI_SHA256=9a18280d10cde9b2e4762de9ca92b7fb1efe83e0637d5f1d4d49720c63d7fd03
RUN mkdir -p /CLIProxyAPI/static \
    && curl -fsSL "https://github.com/router-for-me/Cli-Proxy-API-Management-Center/releases/download/${MANAGEMENT_UI_VERSION}/management.html" -o /CLIProxyAPI/static/management.html \
    && echo "${MANAGEMENT_UI_SHA256}  /CLIProxyAPI/static/management.html" | sha256sum -c - \
    && chmod 644 /CLIProxyAPI/static/management.html
