# =====================
# Build stage
# =====================
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Download modules (cached layer)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -X main.version=$(git describe --tags --always 2>/dev/null || echo 'dev')" \
    -o /app/bin/fiber-api \
    ./cmd/server/main.go

# =====================
# Runtime stage
# =====================
FROM alpine:3.20

WORKDIR /app

# Security: create non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata curl

# Copy binary and assets
COPY --from=builder /app/bin/fiber-api .
COPY --from=builder /app/.env.example .env.example

# Create storage directories
RUN mkdir -p storage/uploads storage/logs && \
    chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/api/ping || exit 1

# Run server
CMD ["./fiber-api"]
