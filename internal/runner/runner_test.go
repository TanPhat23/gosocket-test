package runner_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/runner"
)

func ptr(s string) *string { return &s }

func TestOpenCodeRunnerCommand(t *testing.T) {
	r := runner.NewOpenCodeRunner()
	task := &models.Task{
		Prompt: "Implement unit tests",
		Model:  ptr("gemini-3.8-flash"),
	}

	cmd := r.BuildCommand(task)
	expected := []string{"opencode", "run", "Implement unit tests", "--model", "gemini-3.8-flash"}

	if len(cmd) != len(expected) || cmd[2] != expected[2] {
		t.Errorf("unexpected command: %v", cmd)
	}

	if r.Name() != "opencode" {
		t.Errorf("unexpected name: %s", r.Name())
	}

	if r.DefaultImage() == "" {
		t.Errorf("expected non-empty default image")
	}

	containerCfg, err := r.GenerateContainerConfig(task)
	if err != nil {
		t.Fatalf("GenerateContainerConfig failed: %v", err)
	}
	if containerCfg == nil || containerCfg.Image != r.DefaultImage() {
		t.Errorf("unexpected container config: %+v", containerCfg)
	}
}

func TestOpenCodeRunnerWithoutModel(t *testing.T) {
	r := runner.NewOpenCodeRunner()
	task := &models.Task{
		Prompt: "Simple task",
	}

	cmd := r.BuildCommand(task)
	expected := []string{"opencode", "run", "Simple task"}
	if len(cmd) != len(expected) || cmd[2] != expected[2] {
		t.Errorf("unexpected command without model: %v", cmd)
	}
}

func TestClaudeRunnerPermissions(t *testing.T) {
	r := runner.NewClaudeRunner()
	task := &models.Task{
		Prompt: "Fix security bug",
	}

	cmd := r.BuildCommand(task)
	// Must NOT contain --dangerously-skip-permissions by default
	for _, arg := range cmd {
		if strings.Contains(arg, "dangerously-skip-permissions") {
			t.Fatalf("Claude runner must not contain dangerously-skip-permissions by default: %v", cmd)
		}
	}

	// Verify command format
	if len(cmd) < 3 || cmd[0] != "claude" || cmd[1] != "-p" || cmd[2] != "Fix security bug" {
		t.Errorf("unexpected Claude command: %v", cmd)
	}

	if r.Name() != "claude" {
		t.Errorf("unexpected name: %s", r.Name())
	}

	// When DangerouslySkipPermissions is explicitly enabled via runner option/config
	unsafeRunner := runner.NewClaudeRunner(runner.WithClaudeSkipPermissions(true))
	unsafeCmd := unsafeRunner.BuildCommand(task)
	foundSkip := false
	for _, arg := range unsafeCmd {
		if arg == "--dangerously-skip-permissions" {
			foundSkip = true
			break
		}
	}
	if !foundSkip {
		t.Errorf("expected --dangerously-skip-permissions when explicitly enabled, got: %v", unsafeCmd)
	}
}

func TestCodexRunnerCommand(t *testing.T) {
	r := runner.NewCodexRunner()
	task := &models.Task{
		Prompt: "Write documentation",
	}

	cmd := r.BuildCommand(task)
	expected := []string{"codex", "exec", "Write documentation"}
	if len(cmd) != len(expected) || cmd[0] != "codex" || cmd[1] != "exec" || cmd[2] != "Write documentation" {
		t.Errorf("unexpected codex command: %v", cmd)
	}

	if r.Name() != "codex" {
		t.Errorf("unexpected name: %s", r.Name())
	}

	cfg, err := r.GenerateContainerConfig(task)
	if err != nil {
		t.Fatalf("GenerateContainerConfig failed: %v", err)
	}
	if cfg.Image != r.DefaultImage() {
		t.Errorf("unexpected container config image: %s", cfg.Image)
	}
}

func TestNewRunnerFactory(t *testing.T) {
	runners := []string{"opencode", "claude", "codex"}
	for _, name := range runners {
		r, err := runner.NewRunner(name)
		if err != nil {
			t.Errorf("failed to create runner for %s: %v", name, err)
		}
		if r.Name() != name {
			t.Errorf("expected runner name %s, got %s", name, r.Name())
		}
	}

	_, err := runner.NewRunner("unsupported-agent")
	if err == nil {
		t.Errorf("expected error for unsupported agent runner")
	}
}

func TestInjectSandboxedConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "runner-inject-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	skills := []string{"tdd", "git-workflow"}
	mcpServers := []string{"postgres", "github"}

	err = runner.InjectSandboxedConfig(tmpDir, skills, mcpServers)
	if err != nil {
		t.Fatalf("InjectSandboxedConfig failed: %v", err)
	}

	// 1. Verify opencode.json
	opencodePath := filepath.Join(tmpDir, "opencode.json")
	data, err := os.ReadFile(opencodePath)
	if err != nil {
		t.Fatalf("failed to read opencode.json: %v", err)
	}

	var opencodeCfg struct {
		Skills     []string `json:"skills"`
		MCPServers []string `json:"mcp_servers"`
	}
	if err := json.Unmarshal(data, &opencodeCfg); err != nil {
		t.Fatalf("invalid opencode.json json: %v", err)
	}
	if len(opencodeCfg.Skills) != 2 || len(opencodeCfg.MCPServers) != 2 {
		t.Errorf("unexpected opencode json content: %+v", opencodeCfg)
	}

	// 2. Verify .claude/config.json
	claudePath := filepath.Join(tmpDir, ".claude", "config.json")
	cData, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("failed to read .claude/config.json: %v", err)
	}

	var claudeCfg struct {
		Skills     []string `json:"skills"`
		MCPServers []string `json:"mcpServers"`
	}
	if err := json.Unmarshal(cData, &claudeCfg); err != nil {
		t.Fatalf("invalid .claude/config.json: %v", err)
	}
	if len(claudeCfg.Skills) != 2 || len(claudeCfg.MCPServers) != 2 {
		t.Errorf("unexpected claude json content: %+v", claudeCfg)
	}

	// 3. Ensure no secrets leaked
	rawStr := string(data) + string(cData)
	if strings.Contains(strings.ToLower(rawStr), "api_key") || strings.Contains(strings.ToLower(rawStr), "secret") {
		t.Errorf("config files must not contain secrets or API keys: %s", rawStr)
	}
}

func TestNilTaskHandling(t *testing.T) {
	runners := []runner.AgentRunner{
		runner.NewOpenCodeRunner(),
		runner.NewClaudeRunner(),
		runner.NewCodexRunner(),
	}

	for _, r := range runners {
		if _, err := r.GenerateContainerConfig(nil); err == nil {
			t.Errorf("expected error for nil task in runner %s", r.Name())
		}
		cmd := r.BuildCommand(nil)
		if len(cmd) == 0 {
			t.Errorf("expected fallback command for nil task in runner %s", r.Name())
		}
		env := r.BuildEnv(nil)
		if len(env) != 0 {
			t.Errorf("expected empty env for nil task in runner %s", r.Name())
		}
	}
}

func TestRunnerCustomImageAndEnv(t *testing.T) {
	openCode := runner.NewOpenCodeRunner(runner.WithOpenCodeImage("custom-opencode:v1"))
	if openCode.DefaultImage() != "custom-opencode:v1" {
		t.Errorf("expected custom opencode image, got %s", openCode.DefaultImage())
	}

	claude := runner.NewClaudeRunner(runner.WithClaudeImage("custom-claude:v1"))
	if claude.DefaultImage() != "custom-claude:v1" {
		t.Errorf("expected custom claude image, got %s", claude.DefaultImage())
	}

	codex := runner.NewCodexRunner(runner.WithCodexImage("custom-codex:v1"))
	if codex.DefaultImage() != "custom-codex:v1" {
		t.Errorf("expected custom codex image, got %s", codex.DefaultImage())
	}

	task := &models.Task{
		ID:        "task-123",
		ProjectID: "proj-abc",
		Prompt:    "echo test",
		Model:     ptr("gpt-4o"),
	}

	env := openCode.BuildEnv(task)
	expectedEnv := map[string]string{
		"ATM_TASK_ID":    "task-123",
		"ATM_PROJECT_ID": "proj-abc",
		"ATM_TASK_MODEL": "gpt-4o",
	}

	foundMap := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			foundMap[parts[0]] = parts[1]
		}
	}

	for k, expectedVal := range expectedEnv {
		if foundMap[k] != expectedVal {
			t.Errorf("expected env %s=%s, got %s", k, expectedVal, foundMap[k])
		}
	}
}

func TestClaudeRunnerWithModel(t *testing.T) {
	r := runner.NewClaudeRunner()
	task := &models.Task{
		Prompt: "Debug performance",
		Model:  ptr("claude-3-7-sonnet"),
	}

	cmd := r.BuildCommand(task)
	// Expect ["claude", "-p", "Debug performance", "--model", "claude-3-7-sonnet"]
	if len(cmd) != 5 || cmd[0] != "claude" || cmd[1] != "-p" || cmd[2] != "Debug performance" || cmd[3] != "--model" || cmd[4] != "claude-3-7-sonnet" {
		t.Errorf("unexpected claude command with model: %v", cmd)
	}
}

func TestCodexRunnerWithModel(t *testing.T) {
	r := runner.NewCodexRunner()
	task := &models.Task{
		Prompt: "Write python script",
		Model:  ptr("o3-mini"),
	}

	cmd := r.BuildCommand(task)
	// Expect ["codex", "exec", "Write python script", "--model", "o3-mini"]
	if len(cmd) != 5 || cmd[0] != "codex" || cmd[1] != "exec" || cmd[2] != "Write python script" || cmd[3] != "--model" || cmd[4] != "o3-mini" {
		t.Errorf("unexpected codex command with model: %v", cmd)
	}
}

func TestInjectSandboxedConfigValidation(t *testing.T) {
	if err := runner.InjectSandboxedConfig("", nil, nil); err == nil {
		t.Errorf("expected error for empty worktree path")
	}
}
