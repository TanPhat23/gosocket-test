FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
    curl \
    jq \
    build-essential \
    nodejs \
    npm \
    python3 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=golang:alpine /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

RUN git config --global --add safe.directory '*'

WORKDIR /workspace
