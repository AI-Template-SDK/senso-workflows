# Dockerfile
FROM golang:1.24-alpine AS builder

# 1. Install git and the openssh client
RUN apk add --no-cache git openssh-client

WORKDIR /app

# 3. Configure git to use SSH for GitHub URLs, just like in your setup guide
RUN git config --global url."https://@github.com/".insteadOf "https://github.com/"

# 4. Copy go.mod and go.sum
COPY go.mod go.sum ./

# 5. Run go mod download, securely mounting your local SSH agent for this command only
RUN --mount=type=ssh go mod download

# --- The rest of the Dockerfile remains the same ---
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o senso-workflows .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/senso-workflows .

EXPOSE 8080
CMD ["./senso-workflows"]