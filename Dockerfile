# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Cache dependency downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 — pure-Go SQLite driver requires no C toolchain.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o tinydm ./cmd/tinydm

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.19

# ca-certificates: needed for any outbound TLS (future S3 backend etc.)
# tzdata:          correct timestamps in logs
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/tinydm .

EXPOSE 8080

# Data volume — mount a host directory here for persistence.
VOLUME ["/data"]

ENV TINYDM_HOST=0.0.0.0
ENV TINYDM_PORT=8080
ENV TINYDM_DB_PATH=/data/tinydm.db
ENV TINYDM_STORAGE_PATH=/data/content

ENTRYPOINT ["./tinydm"]
