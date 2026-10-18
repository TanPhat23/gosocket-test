package transfer_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"

	"agent-task-manager/internal/git/transfer"
	"agent-task-manager/internal/sandbox"
)

const testHelperImage = "atm-helper:latest"

// frame encodes one payload as a multiplexed Docker attach frame. Stream 1 is stdout and 2 is stderr.
func frame(stream byte, payload string) string {
	hdr := make([]byte, 8)
	hdr[0] = stream
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(payload)))
	return string(hdr) + payload
}

// scriptedDocker answers every attach with the given stdout and stderr, and exits with the mock's status.
type scriptedDocker struct {
	*sandbox.MockDockerClient
	stdout string
	stderr string
}

func (d *scriptedDocker) ContainerAttach(ctx context.Context, id string, o container.AttachOptions) (types.HijackedResponse, error) {
	if _, err := d.MockDockerClient.ContainerAttach(ctx, id, o); err != nil {
		return types.HijackedResponse{}, err
	}
	conn := &sandbox.MockHijackedConn{}
	conn.OutBuf.WriteString(frame(1, d.stdout) + frame(2, d.stderr))
	return types.NewHijackedResponse(conn, "application/vnd.docker.raw-stream"), nil
}

// cancelDocker cancels the operation as soon as it waits on its helper and never reports an exit, so the operation
// can end only through its cancellation path. It records the context each helper is removed with.
type cancelDocker struct {
	*sandbox.MockDockerClient
	cancel       context.CancelFunc
	removeCtxErr []error
	removeForce  []bool
}

func (d *cancelDocker) ContainerWait(context.Context, string, container.WaitCondition) (<-chan container.WaitResponse, <-chan error) {
	d.cancel()
	return make(chan container.WaitResponse), make(chan error)
}

func (d *cancelDocker) ContainerRemove(ctx context.Context, _ string, o container.RemoveOptions) error {
	d.removeCtxErr = append(d.removeCtxErr, ctx.Err())
	d.removeForce = append(d.removeForce, o.Force)
	return nil
}

func ingestRequest() transfer.IngestRequest {
	return transfer.IngestRequest{
		ProjectID: "repo-1", RepoID: "repo-1", TaskID: "task-1",
		RemoteURL: "https://github.com/owner/repo.git", BaseRef: "refs/heads/main", AuthToken: "tok",
	}
}

// Each operation starts a fixed number of helper containers. A change that adds one must update this table.
func TestHelperContainersPerOperation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		stdout string
		want   int
		run    func(m transfer.GitTransferManager) error
	}{
		{"ingest with a fresh base", "30\n", 2, func(m transfer.GitTransferManager) error {
			_, err := m.IngestRemoteBranch(ctx, ingestRequest())
			return err
		}},
		{"ingest with a stale base", "600\n", 3, func(m transfer.GitTransferManager) error {
			_, err := m.IngestRemoteBranch(ctx, ingestRequest())
			return err
		}},
		{"ingest with no synced base", "missing\n", 3, func(m transfer.GitTransferManager) error {
			_, err := m.IngestRemoteBranch(ctx, ingestRequest())
			return err
		}},
		{"export and push", "", 2, func(m transfer.GitTransferManager) error {
			_, err := m.ExportAndPushTask(ctx, transfer.PushRequest{
				ProjectID: "repo-1", RepoID: "repo-1", TaskID: "task-1",
				RemoteURL: "https://github.com/owner/repo.git", AuthToken: "tok",
			})
			return err
		}},
		{"diff", "", 1, func(m transfer.GitTransferManager) error {
			return m.DiffTask(ctx, transfer.DiffRequest{ProjectID: "repo-1", RepoID: "repo-1", TaskID: "task-1", BaseRef: "refs/heads/main"}, &bytes.Buffer{})
		}},
		{"list", "source=workspace\n", 1, func(m transfer.GitTransferManager) error {
			_, err := m.ListWorkspaceFiles(ctx, transfer.WorkspaceRequest{ProjectID: "repo-1", RepoID: "repo-1", TaskID: "task-1"})
			return err
		}},
		{"read", "missing\n", 1, func(m transfer.GitTransferManager) error {
			_, err := m.ReadWorkspaceFile(ctx, transfer.WorkspaceFileRequest{ProjectID: "repo-1", RepoID: "repo-1", TaskID: "task-1", Path: "a.go", MaxBytes: 10})
			return err
		}},
		{"remove workspace", "", 1, func(m transfer.GitTransferManager) error {
			return m.RemoveTaskWorkspace(ctx, transfer.WorkspaceRequest{ProjectID: "repo-1", RepoID: "repo-1", TaskID: "task-1"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &scriptedDocker{MockDockerClient: sandbox.NewMockDockerClient(), stdout: tc.stdout}
			m := transfer.NewGitTransferManager(
				transfer.NewVolumeTransferHelper(d, testHelperImage),
				transfer.NewCredentialedGitWorker(d, testHelperImage),
			)
			if err := tc.run(m); err != nil {
				t.Fatalf("operation failed: %v", err)
			}
			if got := len(d.CreateCalls); got != tc.want {
				t.Errorf("started %d helper containers, want %d", got, tc.want)
			}
		})
	}
}

// Every helper is removed even when its operation is cancelled while the helper runs. The removal must not inherit the
// cancelled context, or the container is left behind.
func TestHelperIsRemovedWhenTheOperationIsCancelled(t *testing.T) {
	ops := map[string]func(ctx context.Context, d *cancelDocker) error{
		"list": func(ctx context.Context, d *cancelDocker) error {
			_, err := transfer.NewVolumeTransferHelper(d, testHelperImage).ListWorkspaceFiles(ctx, "repo-1", "task-1")
			return err
		},
		"apply bundle": func(ctx context.Context, d *cancelDocker) error {
			_, err := transfer.NewVolumeTransferHelper(d, testHelperImage).ApplyBundleAndProvisionWorktree(
				ctx, "repo-1", "task-1", "refs/heads/main", bytes.NewReader([]byte("# v2 git bundle\n")))
			return err
		},
		"fetch bundle": func(ctx context.Context, d *cancelDocker) error {
			return transfer.NewCredentialedGitWorker(d, testHelperImage).FetchRemoteRefToBundle(
				ctx, "https://github.com/owner/repo.git", "refs/heads/main", "tok", io.Discard)
		},
		"import bundle": func(ctx context.Context, d *cancelDocker) error {
			return transfer.NewCredentialedGitWorker(d, testHelperImage).ImportBundleAndPush(
				ctx, "https://github.com/owner/repo.git", "task-1", "tok", bytes.NewReader([]byte("# v2 git bundle\n")))
		},
	}
	for name, run := range ops {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			d := &cancelDocker{MockDockerClient: sandbox.NewMockDockerClient(), cancel: cancel}

			if err := run(ctx, d); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected the cancellation to end the operation, got %v", err)
			}
			if len(d.removeCtxErr) != 1 {
				t.Fatalf("expected one helper removal, got %d", len(d.removeCtxErr))
			}
			if d.removeCtxErr[0] != nil {
				t.Errorf("the helper was removed with a cancelled context: %v", d.removeCtxErr[0])
			}
			if !d.removeForce[0] {
				t.Errorf("the helper must be force-removed so a still-running helper is stopped")
			}
		})
	}
}

func TestListWorkspaceFilesReturnsTheWorkspaceFiles(t *testing.T) {
	d := &scriptedDocker{MockDockerClient: sandbox.NewMockDockerClient(), stdout: "source=workspace\na.go\x00src/b.go\x00"}
	listing, err := transfer.NewVolumeTransferHelper(d, testHelperImage).ListWorkspaceFiles(context.Background(), "repo-1", "task-1")
	if err != nil {
		t.Fatalf("ListWorkspaceFiles: %v", err)
	}
	if listing.Source != "workspace" || !reflect.DeepEqual(listing.Files, []string{"a.go", "src/b.go"}) {
		t.Errorf("unexpected listing: %+v", listing)
	}
}

// A listing past the cap is refused rather than cut, so a caller never shows a half-written path.
func TestListWorkspaceFilesRefusesAListingPastTheCap(t *testing.T) {
	const capBytes = 16 << 20
	d := &scriptedDocker{MockDockerClient: sandbox.NewMockDockerClient(), stdout: "source=workspace\n" + strings.Repeat("a", capBytes)}
	_, err := transfer.NewVolumeTransferHelper(d, testHelperImage).ListWorkspaceFiles(context.Background(), "repo-1", "task-1")
	if !errors.Is(err, transfer.ErrWorkspaceListingTooLarge) {
		t.Fatalf("expected ErrWorkspaceListingTooLarge, got %v", err)
	}
}

// A negative cap must be refused before any container starts; it previously panicked while slicing the content.
func TestReadWorkspaceFileRefusesANegativeCapBeforeAnyContainer(t *testing.T) {
	d := &scriptedDocker{MockDockerClient: sandbox.NewMockDockerClient(), stdout: "source=workspace size=5\nhello"}
	_, err := transfer.NewVolumeTransferHelper(d, testHelperImage).ReadWorkspaceFile(context.Background(), "repo-1", "task-1", "a.go", -1)
	if err == nil {
		t.Fatalf("expected a negative cap to be refused")
	}
	if len(d.CreateCalls) != 0 {
		t.Errorf("a refused read must not start a container, started %d", len(d.CreateCalls))
	}
}

// The sync-age reply is parsed from a bounded buffer, so an oversized reply is refused instead of parsed.
func TestLastSyncAgeRefusesAnOversizedReply(t *testing.T) {
	d := &scriptedDocker{MockDockerClient: sandbox.NewMockDockerClient(), stdout: strings.Repeat("9", 1000)}
	_, found, err := transfer.NewVolumeTransferHelper(d, testHelperImage).LastSyncAge(context.Background(), "repo-1", "refs/heads/main")
	if err == nil || found {
		t.Fatalf("expected an oversized reply to be refused, got found=%v err=%v", found, err)
	}
}
