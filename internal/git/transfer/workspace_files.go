package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"

	"agent-task-manager/internal/sandbox"
)

// ErrInvalidWorkspacePath rejects paths that are absolute, empty, or contain "." or ".." segments.
var ErrInvalidWorkspacePath = errors.New("invalid workspace path")

// ValidateWorkspacePath accepts only a relative path made of ordinary segments, so it cannot name anything outside
// the task workspace before the helper even runs. The helper also resolves symlinks and refuses escapes.
func ValidateWorkspacePath(p string) error {
	if p == "" || len(p) > 1024 || strings.ContainsRune(p, 0) || strings.HasPrefix(p, "/") {
		return fmt.Errorf("%w: %q", ErrInvalidWorkspacePath, p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || seg == ".git" {
			return fmt.Errorf("%w: %q", ErrInvalidWorkspacePath, p)
		}
	}
	return nil
}

// maxWorkspaceListingBytes caps the file listing of one task workspace. Past it the listing is refused rather than
// cut, because a partial list would end in a half-written path.
const maxWorkspaceListingBytes = 16 << 20

// ErrWorkspaceListingTooLarge reports a workspace whose file listing exceeds maxWorkspaceListingBytes.
var ErrWorkspaceListingTooLarge = errors.New("workspace file listing is too large")

// limitedBuffer keeps the first max bytes and reports every write as complete. It records whether any bytes were
// dropped, and keeps accepting writes so the helper's output is still drained.
type limitedBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	room := b.max - b.buf.Len()
	if len(p) > room {
		b.truncated = true
	}
	if room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// ListWorkspaceFiles lists the task's files. A task whose worktree still exists gets its working tree (tracked and
// untracked, minus ignored files such as dependencies). A task whose worktree was cleaned up gets its committed branch.
func (h *volumeTransferHelper) ListWorkspaceFiles(ctx context.Context, repoID, taskID string) (WorkspaceListing, error) {
	if err := ValidateRepoID(repoID); err != nil {
		return WorkspaceListing{}, err
	}
	if err := ValidateTaskID(taskID); err != nil {
		return WorkspaceListing{}, err
	}

	listScript := fmt.Sprintf(`
set -eu
REPO_DIR="/workspaces/%s/repository.git"
TASK_DIR="/workspaces/%s/tasks/%s"
if [ -d "$TASK_DIR" ]; then
    echo "source=workspace"
    GIT_OPTIONAL_LOCKS=0 git -C "$TASK_DIR" -c core.quotePath=false ls-files -z --cached --others --exclude-standard
else
    echo "source=committed"
    git -C "$REPO_DIR" -c core.quotePath=false ls-tree -r -z --name-only "refs/heads/task/%s" 2>/dev/null || true
fi
`, repoID, repoID, taskID, taskID)

	out := &limitedBuffer{max: maxWorkspaceListingBytes}
	if err := h.runCapturedHelper(ctx, h.readOnlyConfig(listScript, nil), h.readOnlyHost(repoID), out, "file listing"); err != nil {
		return WorkspaceListing{}, err
	}
	if out.truncated {
		return WorkspaceListing{}, fmt.Errorf("%w: more than %d bytes", ErrWorkspaceListingTooLarge, maxWorkspaceListingBytes)
	}
	header, rest, _ := strings.Cut(out.buf.String(), "\n")
	listing := WorkspaceListing{Source: strings.TrimPrefix(header, "source=")}
	for _, name := range strings.Split(rest, "\x00") {
		if name != "" {
			listing.Files = append(listing.Files, name)
		}
	}
	return listing, nil
}

// ReadWorkspaceFile returns up to maxBytes of a file in the task workspace. Missing and outside-workspace paths are
// reported through the result, not as errors.
func (h *volumeTransferHelper) ReadWorkspaceFile(ctx context.Context, repoID, taskID, relPath string, maxBytes int64) (*WorkspaceFile, error) {
	if err := ValidateRepoID(repoID); err != nil {
		return nil, err
	}
	if err := ValidateTaskID(taskID); err != nil {
		return nil, err
	}
	if err := ValidateWorkspacePath(relPath); err != nil {
		return nil, err
	}
	// A negative cap would make the content slice below panic, so it is refused before any container starts.
	if maxBytes < 0 {
		return nil, fmt.Errorf("max bytes must not be negative: %d", maxBytes)
	}

	readScript := fmt.Sprintf(`
set -eu
REPO_DIR="/workspaces/%s/repository.git"
TASK_DIR="/workspaces/%s/tasks/%s"
if [ -d "$TASK_DIR" ]; then
    ROOT=$(readlink -f "$TASK_DIR")
    FULL=$(readlink -f "$ROOT/$ATM_FILE_PATH" 2>/dev/null || true)
    if [ -z "$FULL" ]; then echo missing; exit 0; fi
    case "$FULL" in "$ROOT"/*) ;; *) echo denied; exit 0 ;; esac
    if [ ! -f "$FULL" ]; then echo missing; exit 0; fi
    echo "source=workspace size=$(wc -c < "$FULL" | tr -d ' ')"
    head -c "$ATM_FILE_MAX" "$FULL"
else
    if [ "$(git -C "$REPO_DIR" cat-file -t "refs/heads/task/%s:$ATM_FILE_PATH" 2>/dev/null || true)" != "blob" ]; then echo missing; exit 0; fi
    echo "source=committed size=$(git -C "$REPO_DIR" cat-file -s "refs/heads/task/%s:$ATM_FILE_PATH")"
    git -C "$REPO_DIR" cat-file -p "refs/heads/task/%s:$ATM_FILE_PATH" | head -c "$ATM_FILE_MAX"
fi
`, repoID, repoID, taskID, taskID, taskID, taskID)

	env := []string{
		"ATM_FILE_PATH=" + relPath,
		"ATM_FILE_MAX=" + strconv.FormatInt(maxBytes, 10),
	}
	// The header line is short; the content is capped by the script and by this buffer.
	out := &limitedBuffer{max: int(maxBytes) + 512}
	if err := h.runCapturedHelper(ctx, h.readOnlyConfig(readScript, env), h.readOnlyHost(repoID), out, "file read"); err != nil {
		return nil, err
	}

	raw := out.buf.Bytes()
	header, content, _ := bytes.Cut(raw, []byte("\n"))
	file := &WorkspaceFile{Path: relPath}
	switch string(header) {
	case "missing":
		file.Missing = true
		return file, nil
	case "denied":
		file.Denied = true
		return file, nil
	}
	for _, field := range strings.Fields(string(header)) {
		key, value, _ := strings.Cut(field, "=")
		switch key {
		case "source":
			file.Source = value
		case "size":
			size, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("unexpected file read header %q: %w", header, err)
			}
			file.Size = size
		}
	}
	if file.Source == "" {
		return nil, fmt.Errorf("unexpected file read header %q", header)
	}
	if int64(len(content)) > maxBytes {
		content = content[:maxBytes]
	}
	file.Truncated = file.Size > maxBytes
	file.Binary = bytes.IndexByte(content, 0) >= 0
	if !file.Binary {
		file.Content = content
	}
	return file, nil
}

// readOnlyConfig is the container config shared by the read-only workspace operations.
func (h *volumeTransferHelper) readOnlyConfig(script string, env []string) *container.Config {
	return &container.Config{
		Image:        h.imageTag,
		User:         "1000:1000",
		AttachStdout: true,
		AttachStderr: true,
		Env:          env,
		Cmd:          []string{"/bin/sh", "-c", script},
	}
}

// readOnlyHost is the host config for a read-only, offline look at one project volume.
func (h *volumeTransferHelper) readOnlyHost(repoID string) *container.HostConfig {
	return &container.HostConfig{
		NetworkMode:    "none",
		CapDrop:        []string{"ALL"},
		ReadonlyRootfs: true,
		SecurityOpt:    defaultSecurityOpt(),
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeVolume,
				Source:   sandbox.ProjectDataVolumeName(repoID),
				Target:   "/workspaces",
				ReadOnly: true,
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
			},
		},
	}
}

// ListWorkspaceFiles validates the request and lists the task's files from the volume helper.
func (m *gitTransferManager) ListWorkspaceFiles(ctx context.Context, req WorkspaceRequest) (WorkspaceListing, error) {
	return m.volumeHelper.ListWorkspaceFiles(ctx, req.RepoID, req.TaskID)
}

// ReadWorkspaceFile validates the request and reads one file from the task workspace.
func (m *gitTransferManager) ReadWorkspaceFile(ctx context.Context, req WorkspaceFileRequest) (*WorkspaceFile, error) {
	if err := ValidateWorkspacePath(req.Path); err != nil {
		return nil, err
	}
	file, err := m.volumeHelper.ReadWorkspaceFile(ctx, req.RepoID, req.TaskID, req.Path, req.MaxBytes)
	if err != nil {
		return nil, err
	}
	return file, nil
}
