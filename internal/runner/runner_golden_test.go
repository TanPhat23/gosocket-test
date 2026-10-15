package runner_test

import (
	"reflect"
	"testing"

	"agent-task-manager/internal/models"
	"agent-task-manager/internal/runner"

	"github.com/docker/docker/api/types/container"
)

func TestRunnerGoldenContainerConfig(t *testing.T) {
	task := &models.Task{ID: "t1", ProjectID: "p1", Prompt: "do it", Model: ptr("m1")}
	env := []string{"ATM_TASK_ID=t1", "ATM_PROJECT_ID=p1", "ATM_TASK_MODEL=m1"}
	cases := []struct {
		r     runner.AgentRunner
		name  string
		image string
		cmd   []string
		nilC  []string
	}{
		{runner.NewOpenCodeRunner(), "opencode", "opencode-agent:latest", []string{"opencode", "run", "do it", "--model", "m1"}, []string{"opencode"}},
		{runner.NewClaudeRunner(), "claude", "claude-code-agent:latest", []string{"claude", "-p", "do it", "--model", "m1"}, []string{"claude"}},
		{runner.NewCodexRunner(), "codex", "codex-agent:latest", []string{"codex", "exec", "do it", "--model", "m1"}, []string{"codex"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.r.Name() != tc.name || tc.r.DefaultImage() != tc.image {
				t.Errorf("name/image: %s %s", tc.r.Name(), tc.r.DefaultImage())
			}
			cfg, err := tc.r.GenerateContainerConfig(task)
			if err != nil {
				t.Fatal(err)
			}
			want := &container.Config{Image: tc.image, Cmd: tc.cmd, Env: env, WorkingDir: "/workspace", AttachStdout: true, AttachStderr: true}
			if !reflect.DeepEqual(cfg, want) {
				t.Errorf("got %+v want %+v", cfg, want)
			}
			if !reflect.DeepEqual(tc.r.BuildCommand(nil), tc.nilC) {
				t.Errorf("nil cmd: %v", tc.r.BuildCommand(nil))
			}
			if e := tc.r.BuildEnv(nil); e == nil || len(e) != 0 {
				t.Errorf("nil env must be empty non-nil slice: %#v", e)
			}
			if _, err := tc.r.GenerateContainerConfig(nil); err == nil || err.Error() != "task cannot be nil" {
				t.Errorf("nil task err: %v", err)
			}
			// empty image option is ignored; model-less env
			bare, _ := tc.r.GenerateContainerConfig(&models.Task{ID: "t", ProjectID: "p", Prompt: "x"})
			if !reflect.DeepEqual(bare.Env, []string{"ATM_TASK_ID=t", "ATM_PROJECT_ID=p"}) {
				t.Errorf("env: %v", bare.Env)
			}
		})
	}
	if runner.NewOpenCodeRunner(runner.WithOpenCodeImage("")).DefaultImage() != runner.DefaultOpenCodeImage ||
		runner.NewClaudeRunner(runner.WithClaudeImage("")).DefaultImage() != runner.DefaultClaudeImage ||
		runner.NewCodexRunner(runner.WithCodexImage("")).DefaultImage() != runner.DefaultCodexImage {
		t.Errorf("empty image option must be ignored")
	}
}
