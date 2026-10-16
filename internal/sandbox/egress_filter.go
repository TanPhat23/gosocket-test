package sandbox

import (
	"errors"
	"net"
	"net/url"
)

// EgressPolicy defines egress network filtering rules and forward proxy configuration.
type EgressPolicy struct {
	BlockedCIDRs      []string // e.g. 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, fc00::/7
	BlockedIPs        []string // e.g. 169.254.169.254, fd00:ec2::254
	ForwardProxyURL   string   // e.g. "http://172.28.200.10:3128"; must be an IP literal (see ValidateForwardProxyURL)
	AllowedRegistries []string // e.g. "registry.npmjs.org", "pypi.org", "proxy.golang.org"
}

// DefaultEgressPolicy returns a hardened default-deny egress policy with RFC 1918 CIDRs,
// IPv6 unique local addresses, link-local addresses, and cloud metadata endpoints.
func DefaultEgressPolicy(proxyURL string) *EgressPolicy {
	return &EgressPolicy{
		BlockedCIDRs: []string{
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"169.254.0.0/16", // IPv4 Link-local
			"fc00::/7",       // IPv6 Unique Local Address
			"fe80::/10",      // IPv6 Link-local
		},
		BlockedIPs: []string{
			"169.254.169.254", // Cloud provider metadata endpoint (IPv4)
			"fd00:ec2::254",   // AWS IMDSv6 metadata endpoint
		},
		ForwardProxyURL: proxyURL,
		AllowedRegistries: []string{
			"registry.npmjs.org",
			"pypi.org",
			"proxy.golang.org",
		},
	}
}

// ValidateForwardProxyURL requires an http URL whose host is an IP literal with an explicit port.
// Sandboxes have no working DNS (their resolver points at a dead address), so a hostname such as
// "atm-egress-proxy" could never be resolved and would leave package installs broken.
func ValidateForwardProxyURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return errors.New("egress proxy URL must be http://<ip>:<port>")
	}
	host, port := u.Hostname(), u.Port()
	if net.ParseIP(host) == nil || port == "" || port == "0" {
		return errors.New("egress proxy URL must be http://<ip>:<port> with an IP literal, not a hostname")
	}
	return nil
}

// ValidateDestination checks whether targetIP is allowed or blocked under the egress policy.
// Returns true if destination is allowed, or false if it is blocked or invalid (fail-closed).
func ValidateDestination(policy *EgressPolicy, targetIP string) bool {
	if policy == nil {
		return false
	}

	parsedIP := net.ParseIP(targetIP)
	if parsedIP == nil {
		return false
	}

	// Check explicit blocked IPs
	for _, blockedStr := range policy.BlockedIPs {
		blockedIP := net.ParseIP(blockedStr)
		if blockedIP != nil && blockedIP.Equal(parsedIP) {
			return false
		}
	}

	// Check blocked CIDRs
	for _, cidr := range policy.BlockedCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipNet.Contains(parsedIP) {
			return false
		}
	}

	return true
}

// BuildProxyEnv returns proxy environment variables for container environments.
func BuildProxyEnv(policy *EgressPolicy) []string {
	if policy == nil || policy.ForwardProxyURL == "" {
		return nil
	}

	return []string{
		"HTTP_PROXY=" + policy.ForwardProxyURL,
		"HTTPS_PROXY=" + policy.ForwardProxyURL,
		"http_proxy=" + policy.ForwardProxyURL,
		"https_proxy=" + policy.ForwardProxyURL,
		"NO_PROXY=localhost,127.0.0.1",
	}
}
