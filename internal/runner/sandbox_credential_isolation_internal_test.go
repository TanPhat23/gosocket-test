package runner

import (
	"strings"
	"testing"
)

func TestScrubSandboxEnv_DropsCredentials(t *testing.T) {
	in := []string{
		"HOME=/root", "OPENCODE_PASSWORD=pw", "OPENCODE_SERVER_PASSWORD=pw",
		"ANTHROPIC_API_KEY=k", "openai_api_key=k", "AZURE_OPENAI_API_KEY=k", "ATM_TASK_ID=t1",
	}
	out := scrubSandboxEnv(in)
	got := strings.Join(out, ",")
	for _, bad := range []string{"PASSWORD", "API_KEY", "api_key"} {
		if strings.Contains(got, bad) {
			t.Errorf("credential %q survived: %s", bad, got)
		}
	}
	if got != "HOME=/root,ATM_TASK_ID=t1" {
		t.Errorf("unexpected env: %s", got)
	}
}

func TestAgentSandboxBinds_NoHostOpenCodeProfile(t *testing.T) {
	binds := agentSandboxBinds("/data/wt", "/opt/opencode")
	if len(binds) < 3 {
		t.Fatalf("binds = %v", binds)
	}
	for _, b := range binds {
		if strings.Contains(b, "/root/.config/opencode") || strings.Contains(b, "/root/.local/share/opencode") {
			t.Errorf("host OpenCode profile (may hold auth.json) mounted: %q", b)
		}
	}
	joined := strings.Join(binds, " ")
	for _, want := range []string{"/data/wt:/workspace:rw", "/opt/opencode:/usr/local/bin/opencode:ro", "/data/wt:/data/wt:rw"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing bind %q in %v", want, binds)
		}
	}
}
