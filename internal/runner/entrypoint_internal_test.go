package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runEntrypoint(t *testing.T, secretBody *string, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "entry.sh")
	if err := os.WriteFile(script, embeddedEntrypoint, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(dir, "opencode")
	if secretBody != nil {
		if err := os.WriteFile(secret, []byte(*secretBody), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "ATM_SECRET_FILE=" + secret, "ATM_SECRET_WAIT_SECONDS=1"}
	out, err := cmd.CombinedOutput()
	if secretBody != nil && *secretBody != "" {
		if _, statErr := os.Stat(secret); statErr == nil {
			t.Error("secret file must be unlinked after it is read")
		}
	}
	return string(out), err
}

func TestEntrypoint_ExportsPasswordAndUnlinks(t *testing.T) {
	pw := "s3cr3t-value"
	out, err := runEntrypoint(t, &pw, "sh", "-c", `printf %s "$OPENCODE_PASSWORD"`)
	if err != nil || out != pw {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestEntrypoint_FailsClosedWithoutSecret(t *testing.T) {
	out, err := runEntrypoint(t, nil, "sh", "-c", "echo started")
	if err == nil || strings.Contains(out, "started") {
		t.Fatalf("command must not start without a secret: out=%q err=%v", out, err)
	}
}

func TestEntrypoint_FailsClosedOnEmptySecret(t *testing.T) {
	empty := ""
	out, err := runEntrypoint(t, &empty, "sh", "-c", "echo started")
	if err == nil || strings.Contains(out, "started") {
		t.Fatalf("empty secret must fail closed: out=%q err=%v", out, err)
	}
}

func TestEntrypoint_NeverEchoesSecret(t *testing.T) {
	pw := "do-not-log-me"
	out, _ := runEntrypoint(t, &pw, "sh", "-c", "true")
	if strings.Contains(out, pw) {
		t.Errorf("secret leaked to output: %q", out)
	}
}

func TestEntrypoint_PasswordNotInheritedFromCaller(t *testing.T) {
	// A pre-set OPENCODE_PASSWORD in the container env must not be trusted/used.
	dir := t.TempDir()
	script := filepath.Join(dir, "entry.sh")
	_ = os.WriteFile(script, embeddedEntrypoint, 0o755)
	cmd := exec.Command("sh", script, "sh", "-c", "echo started")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "OPENCODE_PASSWORD=preset", "ATM_SECRET_FILE=" + filepath.Join(dir, "missing"), "ATM_SECRET_WAIT_SECONDS=1"}
	out, err := cmd.CombinedOutput()
	if err == nil || strings.Contains(string(out), "started") {
		t.Fatalf("preset env must not bypass the secret file: %q %v", out, err)
	}
}
