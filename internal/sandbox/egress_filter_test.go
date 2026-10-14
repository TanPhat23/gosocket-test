package sandbox_test

import (
	"strings"
	"testing"

	"agent-task-manager/internal/sandbox"
)

func TestEgressFilter(t *testing.T) {
	t.Run("DefaultEgressPolicy contains required default-deny CIDRs and cloud metadata IPs", func(t *testing.T) {
		proxyURL := "http://atm-egress-proxy:3128"
		policy := sandbox.DefaultEgressPolicy(proxyURL)
		if policy == nil {
			t.Fatalf("expected non-nil policy")
		}

		if policy.ForwardProxyURL != proxyURL {
			t.Errorf("expected ForwardProxyURL %q, got %q", proxyURL, policy.ForwardProxyURL)
		}

		// Check RFC 1918 CIDRs and IPv6 ULA
		expectedCIDRs := []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"fc00::/7",
		}
		for _, cidr := range expectedCIDRs {
			found := false
			for _, c := range policy.BlockedCIDRs {
				if c == cidr {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected BlockedCIDRs to contain %q", cidr)
			}
		}

		// Check metadata IPs (IPv4 and IPv6)
		expectedIPs := []string{
			"169.254.169.254",
			"fd00:ec2::254",
		}
		for _, ip := range expectedIPs {
			found := false
			for _, blocked := range policy.BlockedIPs {
				if blocked == ip {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected BlockedIPs to contain %q", ip)
			}
		}

		// Check default allowed registries
		expectedRegistries := []string{
			"registry.npmjs.org",
			"pypi.org",
			"proxy.golang.org",
		}
		for _, reg := range expectedRegistries {
			found := false
			for _, r := range policy.AllowedRegistries {
				if r == reg {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected AllowedRegistries to contain %q", reg)
			}
		}
	})

	t.Run("ValidateDestination blocks RFC 1918, link-local, and metadata IPs", func(t *testing.T) {
		policy := sandbox.DefaultEgressPolicy("http://atm-egress-proxy:3128")

		blockedCases := []string{
			"10.0.0.1",
			"10.255.255.255",
			"172.16.0.5",
			"172.31.255.254",
			"192.168.1.1",
			"169.254.169.254", // Cloud metadata IPv4
			"169.254.1.1",     // IPv4 link-local
			"fd00:ec2::254",   // AWS IMDSv6
			"fc00::1",         // IPv6 ULA
			"fe80::1",         // IPv6 link-local
		}

		for _, ip := range blockedCases {
			if sandbox.ValidateDestination(policy, ip) {
				t.Errorf("expected IP %s to be blocked, but ValidateDestination returned true (allowed)", ip)
			}
		}

		// Public routable IP should be valid/allowed destination
		allowedCases := []string{
			"1.1.1.1",
			"8.8.8.8",
			"140.82.112.4", // GitHub public IP
			"151.101.65.140", // npmjs
		}

		for _, ip := range allowedCases {
			if !sandbox.ValidateDestination(policy, ip) {
				t.Errorf("expected public IP %s to be allowed, but ValidateDestination returned false (blocked)", ip)
			}
		}
	})

	t.Run("ValidateDestination handles invalid IP fail-closed", func(t *testing.T) {
		policy := sandbox.DefaultEgressPolicy("http://atm-egress-proxy:3128")
		if sandbox.ValidateDestination(policy, "not-an-ip") {
			t.Errorf("expected invalid IP string to be blocked fail-closed")
		}
	})

	t.Run("BuildProxyEnv builds standard uppercase and lowercase proxy env vars with NO_PROXY", func(t *testing.T) {
		proxyURL := "http://atm-egress-proxy:3128"
		policy := sandbox.DefaultEgressPolicy(proxyURL)

		env := sandbox.BuildProxyEnv(policy)
		envMap := make(map[string]string)
		for _, e := range env {
			parts := strings.SplitN(e, "=", 2)
			if len(parts) == 2 {
				envMap[parts[0]] = parts[1]
			}
		}

		if envMap["HTTP_PROXY"] != proxyURL {
			t.Errorf("expected HTTP_PROXY=%s, got %s", proxyURL, envMap["HTTP_PROXY"])
		}
		if envMap["HTTPS_PROXY"] != proxyURL {
			t.Errorf("expected HTTPS_PROXY=%s, got %s", proxyURL, envMap["HTTPS_PROXY"])
		}
		if envMap["http_proxy"] != proxyURL {
			t.Errorf("expected http_proxy=%s, got %s", proxyURL, envMap["http_proxy"])
		}
		if envMap["https_proxy"] != proxyURL {
			t.Errorf("expected https_proxy=%s, got %s", proxyURL, envMap["https_proxy"])
		}
		if envMap["NO_PROXY"] != "localhost,127.0.0.1" {
			t.Errorf("expected NO_PROXY='localhost,127.0.0.1', got %s", envMap["NO_PROXY"])
		}
	})
}
