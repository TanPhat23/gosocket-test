# Build stage
FROM golang:alpine AS builder

WORKDIR /app

# Download dependencies first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build statically linked binaries
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /app/bin/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /app/bin/migrate ./cmd/migrate

# Runtime stage
FROM alpine:3.21

# Install ca-certificates, git, docker-cli, nodejs, and npm for task container interactions
RUN apk add --no-cache ca-certificates git docker-cli nodejs npm && \
    git config --global --add safe.directory '*'

WORKDIR /app

# Copy binaries
COPY --from=builder /app/bin/server /app/server
COPY --from=builder /app/bin/migrate /app/migrate

EXPOSE 8080

ENTRYPOINT ["/app/server"]
