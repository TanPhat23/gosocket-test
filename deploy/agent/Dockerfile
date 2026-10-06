FROM alpine:3.21

RUN apk add --no-cache bash ca-certificates git curl jq

COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh && \
    ln -s /usr/local/bin/entrypoint.sh /usr/local/bin/opencode && \
    ln -s /usr/local/bin/entrypoint.sh /usr/local/bin/claude && \
    ln -s /usr/local/bin/entrypoint.sh /usr/local/bin/codex

WORKDIR /workspace

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
