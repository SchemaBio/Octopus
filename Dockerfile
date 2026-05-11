# ──── Stage 1: Build ────
FROM golang:1.23-alpine AS builder

WORKDIR /build

# Install build deps
RUN apk add --no-cache gcc musl-dev

# Cache module downloads
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o octopus ./cmd/server

# ──── Stage 2: Runtime ────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget

ENV TZ=Asia/Shanghai

# Create non-root user
RUN adduser -D -u 1000 octopus

COPY --from=builder /build/octopus /app/octopus

USER octopus
WORKDIR /app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
  CMD wget -q --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/octopus"]
