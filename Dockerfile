# ==============================================================================
# DOCKERFILE PRODUKSI — KOPDES MERAH PUTIH (Go Backend API)
# Multi-stage build dengan image minimal Alpine Linux (Keamanan & Efisiensi)
# ==============================================================================

# --- Tahap 1: Builder ---
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Pasang dependensi esensial untuk kompilasi statis Go
RUN apk add --no-cache git ca-certificates tzdata

# Cache dependensi Go modules
COPY go.mod go.sum ./
RUN go mod download

# Salin seluruh kode program
COPY . .

# Kompilasi binary Go secara statis murni tanpa ketergantungan CGO
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-w -s -extldflags '-static'" \
    -o /build/kopdes-server ./cmd/server

# --- Tahap 2: Runtime Runner ---
FROM alpine:3.20

WORKDIR /app

# Konfigurasi zona waktu WIB (Asia/Jakarta), sertifikat SSL, dan user non-root
RUN apk add --no-cache ca-certificates tzdata curl && \
    cp /usr/share/zoneinfo/Asia/Jakarta /etc/localtime && \
    echo "Asia/Jakarta" > /etc/timezone && \
    addgroup -S kopdesgroup && adduser -S kopdesuser -G kopdesgroup

# Salin binary dari tahap builder
COPY --from=builder /build/kopdes-server /app/kopdes-server
COPY db/migrations /app/db/migrations

# Berikan izin eksekusi kepada user non-root
RUN chown -R kopdesuser:kopdesgroup /app

USER kopdesuser

# Port default yang diekspos (dapat dioverride via .env)
EXPOSE 8080

# Health check otomatis untuk Docker / Kubernetes
HEALTHCHECK --interval=15s --timeout=5s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/app/kopdes-server"]
