# Dockerfile
FROM golang:1.24-alpine AS builder

# Install git for private repo access
RUN apk --no-cache add git

# Accept GitHub PAT as build argument
ARG GITHUB_PAT

# Configure git to use the GitHub PAT for authentication
RUN git config --global url."https://${GITHUB_PAT}@github.com/".insteadOf "https://github.com/"

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o senso-workflows .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/senso-workflows .

# Copy .env file if it exists (from GitHub Actions)
COPY .env* ./

EXPOSE 8000
CMD ["./senso-workflows"] 