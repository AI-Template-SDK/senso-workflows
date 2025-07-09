# Migration Dockerfile
# This container fetches migrations from the senso-api dependency and runs them

FROM golang:1.24-alpine AS builder

# Install git for go mod download
RUN apk --no-cache add git

# Accept GitHub PAT as build argument
ARG GITHUB_PAT

# Configure git to use the GitHub PAT for authentication
RUN git config --global url."https://${GITHUB_PAT}@github.com/".insteadOf "https://github.com/"

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum to download dependencies
COPY go.mod go.sum ./

# Download dependencies (this will fetch senso-api with authentication)
RUN go mod download

# Now we need to extract migrations from the senso-api module
# The module will be in the Go module cache
RUN find /go/pkg/mod -name "*senso-api*" -type d | head -1 | xargs -I {} find {} -name "migrations" -type d | head -1 | xargs -I {} cp -r {} /migrations || \
    (echo "Migrations directory not found in senso-api module" && exit 1)

# Use migrate image to run migrations
FROM migrate/migrate:v4.15.2

# Copy migrations from builder stage
COPY --from=builder /migrations /migrations

# Create a script to run migrations with environment variable
RUN echo '#!/bin/sh' > /run-migrations.sh && \
    echo 'migrate -path /migrations -database "$DATABASE_URL" up' >> /run-migrations.sh && \
    chmod +x /run-migrations.sh

ENTRYPOINT ["/run-migrations.sh"] 