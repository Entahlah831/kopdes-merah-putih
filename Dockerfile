# Tahap 1: Build binary statis Go menggunakan versi terbaru yang ter-patch
FROM golang:1.27.4-alpine3.23 AS builder

WORKDIR /app

# Dependensi build minimal (termasuk sertifikat Root CA & data zona waktu)
# Alasan DL3018: base golang:1.27.4-alpine3.23 sudah di-pin ke Alpine 3.23 dan paket apk mengikuti security patch minor Alpine; seluruh CVE paket diaudit oleh Trivy Image Scan di CI.
# hadolint ignore=DL3018
RUN apk add --no-cache git ca-certificates tzdata

# Cache layer untuk go.mod dan go.sum
COPY go.mod go.sum ./
RUN go mod download

# Salin sumber aplikasi
COPY . .

# Kompilasi binary statis dengan strip simbol
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/kopdes-api ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/kopdes ./cmd/kopdes && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/envcheck ./cmd/envcheck && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/uitester ./cmd/uitester

# Tahap 2: Runtime Container Minimal & Non-Root
FROM alpine:3.23

WORKDIR /app

ARG APP_TIMEZONE=UTC
ENV TZ=${APP_TIMEZONE}

# Alasan DL3018: base image alpine:3.23 sudah di-pin dan paket apk mengikuti security patch minor Alpine; seluruh CVE paket diaudit oleh Trivy Image Scan di CI.
# hadolint ignore=DL3018
RUN apk add --no-cache ca-certificates tzdata curl && \
    addgroup -S kopdes && adduser -S -G kopdes kopdesuser && \
    mkdir -p /app/storage/kyc /app/storage/bukti && chown -R kopdesuser:kopdes /app/storage

COPY --from=builder /app/kopdes-api /app/kopdes-api
COPY --from=builder /app/kopdes /app/kopdes
COPY --from=builder /app/envcheck /app/envcheck
COPY --from=builder /app/uitester /app/uitester
COPY --from=builder /app/frontend-tester /app/frontend-tester
COPY --from=builder /app/db/migrations /app/db/migrations

USER kopdesuser

# Healthcheck membaca variabel API_PORT runtime (tanpa hardcoded port)
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD test -n "${API_PORT:-}" && curl -fsS "http://127.0.0.1:${API_PORT}/healthz" >/dev/null

ENTRYPOINT ["/app/kopdes"]
CMD ["serve"]
