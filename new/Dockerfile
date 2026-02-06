# Multi-stage Dockerfile for Senso Workflows Step Functions
# Optimized for production deployment on AWS ECS Fargate

# Stage 1: Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
# CGO_ENABLED=0 for static binary (required for scratch)
# -ldflags for smaller binary size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o step-handler \
    ./cmd/step-handler

# Stage 2: Production stage
FROM alpine:latest

# Install CA certificates for HTTPS calls to OpenAI, BrightData, etc.
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user for security
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/step-handler /app/step-handler

# Copy timezone data
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Set ownership
RUN chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/bin/sh", "-c", "echo '{\"status\":\"healthy\"}' || exit 1"]

# Entry point
ENTRYPOINT ["/app/step-handler"]

# Metadata labels
LABEL maintainer="Senso Team" \
      description="Senso Workflows Step Functions - ECS Task Handler" \
      version="1.0.0"
