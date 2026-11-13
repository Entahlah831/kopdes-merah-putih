# Tahap 1: Build binary statis Go menggunakan versi terbaru yang ter-patch
FROM golang:1.25.8-alpine3.21 AS builder

WORKDIR /app

# Dependensi build minimal (termasuk sertifikat Root CA & data zona waktu)
RUN apk add --no-cache git ca-certificates tzdata

# Cache layer untuk go.mod dan go.sum
COPY go.mod go.sum ./
RUN go mod download

# Salin sumber aplikasi
COPY . .

# Kompilasi binary statis dengan strip simbol
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/kopdes-api ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/envcheck ./cmd/envcheck

# Tahap 2: Runtime Container Minimal & Non-Root
FROM alpine:3.21

WORKDIR /app

ARG APP_TIMEZONE=UTC
ENV TZ=${APP_TIMEZONE}

RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -S kopdes && adduser -S -G kopdes kopdesuser && \
    mkdir -p /app/storage/kyc && chown -R kopdesuser:kopdes /app/storage

COPY --from=builder /app/kopdes-api /app/kopdes-api
COPY --from=builder /app/envcheck /app/envcheck
COPY --from=builder /app/db/migrations /app/db/migrations

USER kopdesuser

# Healthcheck membaca variabel API_PORT runtime (tanpa hardcoded port)
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD sh -c 'test -n "${API_PORT:-}" && curl -fsS "http://127.0.0.1:${API_PORT}/healthz" >/dev/null'

ENTRYPOINT ["/app/kopdes-api"]
