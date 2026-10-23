package transfer_test

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
)

type GitFixtureServer struct {
	Server   *httptest.Server
	RepoDir  string
	CertFile string
}

func startTestGitHTTPServer(t *testing.T, setupRepo func(string)) *GitFixtureServer {
	t.Helper()
	repoDir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", repoDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init bare test repo: %v", err)
	}

	if setupRepo != nil {
		setupRepo(repoDir)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock smart HTTPS Git protocol handler
		w.Header().Set("Content-Type", "application/x-git-upload-pack-advertisement")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("001e# service=git-upload-pack\n0000"))
	})

	server := httptest.NewTLSServer(handler)
	return &GitFixtureServer{
		Server:  server,
		RepoDir: repoDir,
	}
}

func generateCanaryToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "ghp_ATM_CANARY_" + hex.EncodeToString(b)
}
