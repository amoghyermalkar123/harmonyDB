# Multi-stage build for HarmonyDB testing
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application with proper binary name
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o harmonydb ./cmd/server.go

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates wget

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /app/harmonydb .

# Create directories for data
RUN mkdir -p /var/lib/harmonydb /var/log/harmonydb

# Expose ports
# 8080: HTTP API (default)
# 9090: Raft consensus (default)
EXPOSE 8080 9090

# Set default environment variables
ENV RAFT_PORT=9090
ENV HTTP_PORT=8080
ENV DATA_DIR=/var/lib/harmonydb
ENV LOG_LEVEL=info

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:${HTTP_PORT}/health || exit 1

# Run the binary
ENTRYPOINT ["./harmonydb"]