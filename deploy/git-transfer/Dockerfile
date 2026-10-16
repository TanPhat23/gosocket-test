# Image for the Gate 0C git transfer helper and credentialed worker (internal/git/transfer).
# The base is pinned by digest, the same reference as runner.DefaultBaseImageRef.
# Both containers run as 1000:1000 with a read-only rootfs and HOME=/tmp, so the image needs only
# git, the system CA bundle, and a /workspaces directory owned by 1000:1000: a fresh named volume
# copies that ownership, and the helper fails with ErrVolumeNotWritable (exit 103) without it.
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

RUN apk add --no-cache git ca-certificates \
    && adduser -D -u 1000 transfer \
    && mkdir /workspaces \
    && chown 1000:1000 /workspaces

USER 1000:1000
WORKDIR /workspaces
