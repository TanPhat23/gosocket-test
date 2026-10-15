package runner

import (
	"context"
	"errors"

	"github.com/docker/docker/api/types/image"

	"agent-task-manager/internal/sandbox"
)

// openCodeImageRepoTag is the single source of the OpenCode image repository and tag. The
// pre-build pull and the Dockerfile's COPY --from both use openCodeImageRef, so they cannot drift.
const openCodeImageRepoTag = "ghcr.io/anomalyco/opencode:2.0.26"

// openCodeImageRef returns the digest-pinned OpenCode image reference for digest.
func openCodeImageRef(digest string) string {
	return openCodeImageRepoTag + "@" + digest
}

// requirePinnedRef is a template function that fails closed unless s is a digest-pinned ref
// (the same rule preflight applies to the base image reference).
func requirePinnedRef(s string) (string, error) {
	if !baseImageRefRe.MatchString(s) {
		return "", errors.New("image reference is not digest-pinned")
	}
	return s, nil
}

// pullPinnedImage pulls one digest-pinned image for requiredPlatform and consumes the pull
// stream fail closed. The BuildKit build that follows resolves FROM and COPY --from refs from
// the local image store; without the pre-pull it needs a client session to resolve them.
// Any failure returns ErrImageBuildFailed; engine detail is only logged, sanitized.
func pullPinnedImage(ctx context.Context, cli sandbox.DockerClient, ref string) error {
	if !baseImageRefRe.MatchString(ref) {
		return buildFail("refusing to pull an image reference that is not digest-pinned")
	}
	body, err := cli.ImagePull(ctx, ref, image.PullOptions{Platform: requiredPlatform})
	if err != nil {
		return buildFail("failed to pull pinned image: " + err.Error())
	}
	if body == nil {
		return buildFail("pull of pinned image returned no stream")
	}
	// Cancellation or timeout must abort a blocked stream read: closing the body unblocks Read.
	stopCloser := context.AfterFunc(ctx, func() { _ = body.Close() })
	defer stopCloser()
	defer body.Close()

	// Pull streams carry status/progress (ignored) and error/errorDetail (fatal); the build
	// stream parser applies the same fail-closed rules, including rejecting an empty stream.
	if _, err := consumeBuildStream(body, maxBuildStreamBytes); err != nil {
		buildLogf("runtime image pre-pull failed for %s", sanitizeBuildDetail(ref))
		return err
	}
	return nil
}

// pullPinnedImages pulls refs in order and stops at the first failure.
func pullPinnedImages(ctx context.Context, cli sandbox.DockerClient, refs []string) error {
	for _, ref := range refs {
		if err := pullPinnedImage(ctx, cli, ref); err != nil {
			return err
		}
	}
	return nil
}
