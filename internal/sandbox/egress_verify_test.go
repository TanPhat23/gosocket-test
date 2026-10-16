package sandbox

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func verifyCfg() EgressVerifyConfig {
	return EgressVerifyConfig{Image: "img@sha256:" + strings.Repeat("a", 64), Network: "br-test", Control: "172.28.200.10:3128", Canaries: []string{"192.168.1.1:22", "10.0.0.5:80"}}
}

func TestVerifyEgressFirewall_ExitCodes(t *testing.T) {
	for _, tc := range []struct {
		code int64
		want error
		any  bool
	}{{0, nil, false}, {10, ErrEgressOpen, false}, {3, ErrEgressControlUnreachable, false}, {11, ErrEgressCanaryAnswered, false}, {2, nil, true}, {1, nil, true}, {137, nil, true}} {
		m := NewMockDockerClient()
		m.WaitStatusCode = tc.code
		err := VerifyEgressFirewall(context.Background(), m, verifyCfg())
		switch {
		case tc.code == 0 && err != nil:
			t.Errorf("exit 0: %v", err)
		case tc.want != nil && !errors.Is(err, tc.want):
			t.Errorf("exit %d: want %v got %v", tc.code, tc.want, err)
		case tc.any && err == nil:
			t.Errorf("exit %d must fail closed", tc.code)
		}
	}
}

func TestVerifyEgressFirewall_ProbeIsHardenedAndArgsArePositional(t *testing.T) {
	m := NewMockDockerClient()
	if err := VerifyEgressFirewall(context.Background(), m, verifyCfg()); err != nil {
		t.Fatal(err)
	}
	c := m.CreateCalls[0]
	if c.HostConfig.NetworkMode != "br-test" || c.HostConfig.DNS[0] != SandboxDeadDNS || !c.HostConfig.ReadonlyRootfs ||
		len(c.HostConfig.CapDrop) != 1 || c.HostConfig.CapDrop[0] != "ALL" {
		t.Fatalf("probe not hardened like a sandbox: %+v", c.HostConfig)
	}
	cmd := c.Config.Cmd
	if len(cmd) != 7 || cmd[0] != "/bin/sh" || cmd[4] != "172.28.200.10:3128" || cmd[5] != "192.168.1.1:22" || strings.Contains(cmd[2], "192.168") {
		t.Fatalf("addresses must be positional args, not in the script: %q", cmd)
	}
}

func TestVerifyEgressFirewall_RejectsBadConfigBeforeDocker(t *testing.T) {
	bad := []func(*EgressVerifyConfig){
		func(c *EgressVerifyConfig) { c.Canaries = nil },
		func(c *EgressVerifyConfig) { c.Canaries = []string{"example.com:80"} },
		func(c *EgressVerifyConfig) { c.Canaries = []string{"1.2.3.4:80; reboot"} },
		func(c *EgressVerifyConfig) { c.Canaries = []string{"[::1]:80"} },
		func(c *EgressVerifyConfig) { c.Control = "proxy:3128" },
		func(c *EgressVerifyConfig) { c.Control = "1.2.3.4:0" },
		func(c *EgressVerifyConfig) { c.Image = "" },
		func(c *EgressVerifyConfig) { c.Network = "" },
	}
	for i, mut := range bad {
		m := NewMockDockerClient()
		cfg := verifyCfg()
		mut(&cfg)
		if err := VerifyEgressFirewall(context.Background(), m, cfg); err == nil || len(m.CreateCalls) != 0 {
			t.Errorf("case %d: want error before any Docker call, got %v (%d creates)", i, err, len(m.CreateCalls))
		}
	}
}

func TestVerifyEgressFirewall_DockerFailuresFailClosed(t *testing.T) {
	m := NewMockDockerClient()
	m.StartErr = errors.New("boom")
	if err := VerifyEgressFirewall(context.Background(), m, verifyCfg()); err == nil {
		t.Fatal("start failure must not pass")
	}
	if err := VerifyEgressFirewall(context.Background(), nil, verifyCfg()); err == nil {
		t.Fatal("nil client must not pass")
	}
}

func TestValidateEgressVerifyConfig(t *testing.T) {
	ok := EgressVerifyConfig{Image: "img", Network: "n", Control: "172.28.200.10:3128", Canaries: []string{"10.0.0.1:22"}}
	if err := ValidateEgressVerifyConfig(ok); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	bad := map[string]func(*EgressVerifyConfig){
		"no image":     func(c *EgressVerifyConfig) { c.Image = "" },
		"no network":   func(c *EgressVerifyConfig) { c.Network = "" },
		"bad control":  func(c *EgressVerifyConfig) { c.Control = "proxy:3128" },
		"no canaries":  func(c *EgressVerifyConfig) { c.Canaries = nil },
		"bad canary":   func(c *EgressVerifyConfig) { c.Canaries = []string{"10.0.0.1"} },
		"port too big": func(c *EgressVerifyConfig) { c.Canaries = []string{"10.0.0.1:70000"} },
	}
	for name, mutate := range bad {
		c := ok
		mutate(&c)
		if err := ValidateEgressVerifyConfig(c); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
