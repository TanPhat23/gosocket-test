package runner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"agent-task-manager/internal/gateway"
	"agent-task-manager/internal/sandbox"
)

// ProjectRuntime groups the qualified Gate 0C/0D/0G pieces of the project-scoped runtime: the sandbox
// manager (gateway tokens, firewall proof before start) and the image builder (gateway CA baked in).
//
// Building one does not lift the quarantine: EnsureProjectContainer, RunTask and the workspace manager
// still refuse unconditionally. This is the single place that assembles the pieces, so lifting the
// quarantine later does not need new configuration code.
type ProjectRuntime struct {
	Manager ProjectSandboxManager
	Builder RuntimeImageBuilder
}

// ProjectRuntimeFromEnv builds the ProjectRuntime when ATM_PROJECT_RUNTIME_ENABLED=true. It returns
// nil, nil when off, and an error, never a half-configured runtime, for anything missing or unsafe:
//
//	ATM_SANDBOX_GATEWAY_URL      https://<ip>:<port>/v1 the sandbox uses to reach the gateway (https only)
//	ATM_GATEWAY_CA_FILE          PEM CERTIFICATE(s) of the CA that signed the gateway certificate
//	ATM_SANDBOX_CONTEXT_TOKENS   model context window advertised to OpenCode
//	ATM_SANDBOX_PROXY_URL        http://<ip>:<port> forward proxy, the firewall's one allowed destination
//	ATM_EGRESS_VERIFY_IMAGE      digest-pinned image with busybox nc (deploy/git-transfer/Dockerfile)
//	ATM_EGRESS_CANARIES          comma separated ipv4:port that the firewall must drop
//
// The gateway models and output limit come from the gateway's own ATM_GATEWAY_MODELS and
// ATM_GATEWAY_MAX_OUTPUT_TOKENS so they cannot disagree. The gateway must be enabled.
func ProjectRuntimeFromEnv(
	getenv func(string) string,
	readFile func(string) ([]byte, error),
	cli sandbox.DockerClient,
	catalog RuntimeCatalog,
	gw *gateway.Service,
) (*ProjectRuntime, error) {
	switch getenv("ATM_PROJECT_RUNTIME_ENABLED") {
	case "", "false":
		return nil, nil
	case "true":
	default:
		return nil, errors.New("project runtime: ATM_PROJECT_RUNTIME_ENABLED must be \"true\" or \"false\"")
	}
	if cli == nil {
		return nil, errors.New("project runtime: a Docker client is required")
	}
	if gw == nil {
		return nil, errors.New("project runtime: the LLM gateway must be enabled (ATM_GATEWAY_ENABLED=true)")
	}

	gwURL := getenv("ATM_SANDBOX_GATEWAY_URL")
	if u, err := url.Parse(gwURL); err != nil || u.Scheme != "https" {
		return nil, errors.New("project runtime: ATM_SANDBOX_GATEWAY_URL must be an https URL")
	}
	broker, err := gw.NewBroker(gwURL, 24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("project runtime: %w", err)
	}

	caPEM, err := readFile(getenv("ATM_GATEWAY_CA_FILE"))
	if err != nil {
		return nil, errors.New("project runtime: ATM_GATEWAY_CA_FILE is required and must be readable")
	}
	builder, err := NewRuntimeImageBuilderWithGatewayCA(cli, catalog, caPEM)
	if err != nil {
		return nil, fmt.Errorf("project runtime: ATM_GATEWAY_CA_FILE: %w", err)
	}

	var models []string
	for _, m := range strings.Split(getenv("ATM_GATEWAY_MODELS"), ",") {
		if m = strings.TrimSpace(m); m != "" {
			models = append(models, m)
		}
	}
	ctxTokens, err := positiveInt(getenv("ATM_SANDBOX_CONTEXT_TOKENS"))
	if err != nil {
		return nil, errors.New("project runtime: ATM_SANDBOX_CONTEXT_TOKENS must be a positive integer")
	}
	outTokens, err := positiveInt(getenv("ATM_GATEWAY_MAX_OUTPUT_TOKENS"))
	if err != nil {
		return nil, errors.New("project runtime: ATM_GATEWAY_MAX_OUTPUT_TOKENS must be a positive integer")
	}

	proxyURL := getenv("ATM_SANDBOX_PROXY_URL")
	if err := sandbox.ValidateForwardProxyURL(proxyURL); err != nil {
		return nil, fmt.Errorf("project runtime: ATM_SANDBOX_PROXY_URL: %w", err)
	}
	pu, _ := url.Parse(proxyURL)
	image, err := requirePinnedRef(getenv("ATM_EGRESS_VERIFY_IMAGE"))
	if err != nil {
		return nil, errors.New("project runtime: ATM_EGRESS_VERIFY_IMAGE must be a digest-pinned image reference")
	}
	var canaries []string
	for _, c := range strings.Split(getenv("ATM_EGRESS_CANARIES"), ",") {
		if c = strings.TrimSpace(c); c != "" {
			canaries = append(canaries, c)
		}
	}
	if len(canaries) == 0 {
		return nil, errors.New("project runtime: ATM_EGRESS_CANARIES needs at least one ipv4:port")
	}
	control := net.JoinHostPort(pu.Hostname(), pu.Port())
	// Dry-run the config so a typo fails at startup, not at the first task.
	if err := sandbox.ValidateEgressVerifyConfig(sandbox.EgressVerifyConfig{Image: image, Network: "x", Control: control, Canaries: canaries}); err != nil {
		return nil, fmt.Errorf("project runtime: %w", err)
	}

	mgr := &projectSandboxManager{
		dockerCli:    cli,
		egressPolicy: sandbox.DefaultEgressPolicy(proxyURL),
		gatewayURL:   gwURL,
		gw:           &GatewayAccess{Broker: broker, Models: models, ContextTokens: ctxTokens, OutputTokens: outTokens},
		routes:       make(map[string]*ProjectRouteHandle),
		egressVerify: func(ctx context.Context, network string) error {
			return sandbox.VerifyEgressFirewall(ctx, cli, sandbox.EgressVerifyConfig{
				Image: image, Network: network, Control: control, Canaries: canaries,
			})
		},
	}
	return &ProjectRuntime{Manager: mgr, Builder: builder}, nil
}

func positiveInt(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || strconv.Itoa(n) != s {
		return 0, errors.New("not a positive integer")
	}
	return n, nil
}
