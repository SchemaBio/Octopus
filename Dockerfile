# syntax=docker/dockerfile:1.7

# ---- Stage 1: Build ----
FROM golang:1.25-alpine3.22 AS builder

ENV GOPROXY=https://mirrors.tencent.com/go/,direct

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.tencent.com/g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates git

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w -buildid=" -o /out/octopus ./cmd/server

# ---- Stage 2: Runtime ----
FROM alpine:3.22

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.tencent.com/g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates tzdata wget

ENV TZ=Asia/Shanghai
ENV SERVER_PORT=8080 \
    GIN_MODE=release \
    OUTPUT_DIR=/data/output \
    ARCHIVE_DIR=/data/archive \
    STORAGE_LOCAL_DIR=/data/uploads \
    TEMPLATE_DIR=/data/templates \
    STORAGE_PROVIDER=local

RUN adduser -D -u 1000 octopus \
    && mkdir -p /data/output /data/archive /data/uploads /data/templates \
    && chown -R octopus:octopus /data

WORKDIR /app
COPY --from=builder /out/octopus /app/octopus

USER octopus

EXPOSE 8080

LABEL org.opencontainers.image.title="Octopus" \
      org.opencontainers.image.description="SchemaBio self-hosted analysis backend" \
      org.opencontainers.image.source="https://github.com/SchemaBio/Octopus"

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -q -T 2 --spider http://127.0.0.1:8080/health || exit 1

STOPSIGNAL SIGTERM
ENTRYPOINT ["/app/octopus"]
