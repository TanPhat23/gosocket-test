package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// FirewallAttestation is the exact value ATM_GATEWAY_FIREWALL_ATTESTATION must hold before the gateway
// starts. The gateway speaks plain HTTP to sandboxes (Gate 0G decision B4), which is only safe when the
// host operator has restricted the sandbox bridges to reaching this listener and nothing else. ATM never
// changes firewall rules; the operator states here that they did (see the Gate 0G operator handoff).
const FirewallAttestation = "sandbox-bridge-to-gateway-only"

const (
	defaultMaxBodyBytes    = 1 << 20
	defaultMaxOutputTokens = 4096
	defaultQuotaRequests   = 200
	defaultQuotaTokens     = 500_000
	defaultIdleTimeout     = 60 * time.Second
	chatCompletionsPath    = "/v1/chat/completions"
)

// Service is a configured, not yet listening gateway.
type Service struct {
	Addr    string
	Handler *Handler
	// Issuer mints and revokes the per-attempt capability tokens the Handler accepts.
	Issuer *Issuer
	Ledger *Ledger
}

// ServiceFromEnv builds the gateway from ATM_GATEWAY_* settings. It returns (nil, nil) when the gateway
// is not enabled, and an error, never a partially configured service, for anything unsafe or missing.
// Errors name the offending setting and never include secret values.
func ServiceFromEnv(getenv func(string) string, readFile func(string) ([]byte, error), logf func(string, ...any)) (*Service, error) {
	switch getenv("ATM_GATEWAY_ENABLED") {
	case "", "false":
		return nil, nil
	case "true":
	default:
		return nil, errors.New("gateway: ATM_GATEWAY_ENABLED must be \"true\" or \"false\"")
	}
	if getenv("ATM_GATEWAY_FIREWALL_ATTESTATION") != FirewallAttestation {
		return nil, fmt.Errorf("gateway: ATM_GATEWAY_FIREWALL_ATTESTATION must be %q: the host operator must first install the firewall rule that restricts sandbox bridges to the gateway listener", FirewallAttestation)
	}
	addr, err := listenAddr(getenv("ATM_GATEWAY_LISTEN_ADDR"))
	if err != nil {
		return nil, err
	}
	models := splitList(getenv("ATM_GATEWAY_MODELS"))
	if len(models) == 0 {
		return nil, errors.New("gateway: ATM_GATEWAY_MODELS must list at least one model")
	}
	key, err := providerKey(getenv, readFile)
	if err != nil {
		return nil, err
	}
	issuer, err := NewIssuer([]byte(getenv("ATM_GATEWAY_TOKEN_SECRET")))
	if err != nil {
		return nil, errors.New("gateway: ATM_GATEWAY_TOKEN_SECRET must be set to at least 32 bytes")
	}

	limits := map[string]int64{}
	for name, def := range map[string]int64{
		"ATM_GATEWAY_MAX_BODY_BYTES":    defaultMaxBodyBytes,
		"ATM_GATEWAY_MAX_OUTPUT_TOKENS": defaultMaxOutputTokens,
		"ATM_GATEWAY_QUOTA_REQUESTS":    defaultQuotaRequests,
		"ATM_GATEWAY_QUOTA_TOKENS":      defaultQuotaTokens,
	} {
		if limits[name], err = positiveInt(getenv(name), def, name); err != nil {
			return nil, err
		}
	}
	idle := defaultIdleTimeout
	if v := getenv("ATM_GATEWAY_IDLE_TIMEOUT"); v != "" {
		if idle, err = time.ParseDuration(v); err != nil || idle <= 0 {
			return nil, errors.New("gateway: ATM_GATEWAY_IDLE_TIMEOUT must be a positive duration")
		}
	}

	keyHeader, keyPrefix := "Authorization", "Bearer "
	if h := getenv("ATM_GATEWAY_KEY_HEADER"); h != "" {
		keyHeader, keyPrefix = h, getenv("ATM_GATEWAY_KEY_PREFIX")
	}

	ledger := NewLedger(Quota{MaxRequests: limits["ATM_GATEWAY_QUOTA_REQUESTS"], MaxTokens: limits["ATM_GATEWAY_QUOTA_TOKENS"]})
	handler, err := NewHandler(Config{
		UpstreamBaseURL: getenv("ATM_GATEWAY_UPSTREAM_URL"),
		KeyHeader:       keyHeader,
		KeyPrefix:       keyPrefix,
		Key:             key,
		AllowedPaths:    []string{chatCompletionsPath},
		AllowedModels:   models,
		MaxBodyBytes:    limits["ATM_GATEWAY_MAX_BODY_BYTES"],
		MaxOutputTokens: limits["ATM_GATEWAY_MAX_OUTPUT_TOKENS"],
		IdleTimeout:     idle,
		Issuer:          issuer,
		Ledger:          ledger,
		Logf:            logf,
	})
	if err != nil {
		return nil, err
	}
	return &Service{Addr: addr, Handler: handler, Issuer: issuer, Ledger: ledger}, nil
}

// Listen binds the configured address.
func (s *Service) Listen() (net.Listener, error) { return net.Listen("tcp", s.Addr) }

// Run serves on ln until ctx is cancelled, then shuts down gracefully. It returns nil on a clean shutdown.
func (s *Service) Run(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    16 << 10,
		// No write timeout: model responses stream; the handler enforces its own idle timeout.
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// listenAddr requires an explicit, non-wildcard host and a real port: a wildcard bind would expose
// the plain-HTTP listener on every interface, not just the sandbox-facing one.
func listenAddr(v string) (string, error) {
	host, port, err := net.SplitHostPort(v)
	if err != nil || host == "" {
		return "", errors.New("gateway: ATM_GATEWAY_LISTEN_ADDR must be host:port with an explicit host")
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return "", errors.New("gateway: ATM_GATEWAY_LISTEN_ADDR must not be a wildcard address")
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return "", errors.New("gateway: ATM_GATEWAY_LISTEN_ADDR has an invalid port")
	}
	return v, nil
}

// providerKey returns a per-request key reader from exactly one of ATM_GATEWAY_PROVIDER_KEY or
// ATM_GATEWAY_PROVIDER_KEY_FILE. A file is re-read on every request so rotation needs no restart.
func providerKey(getenv func(string) string, readFile func(string) ([]byte, error)) (func() string, error) {
	direct, path := getenv("ATM_GATEWAY_PROVIDER_KEY"), getenv("ATM_GATEWAY_PROVIDER_KEY_FILE")
	switch {
	case direct != "" && path != "":
		return nil, errors.New("gateway: set only one of ATM_GATEWAY_PROVIDER_KEY and ATM_GATEWAY_PROVIDER_KEY_FILE")
	case direct != "":
		return func() string { return direct }, nil
	case path == "":
		return nil, errors.New("gateway: ATM_GATEWAY_PROVIDER_KEY or ATM_GATEWAY_PROVIDER_KEY_FILE is required")
	}
	read := func() string {
		b, err := readFile(path)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if read() == "" {
		return nil, errors.New("gateway: ATM_GATEWAY_PROVIDER_KEY_FILE is unreadable or empty")
	}
	return read, nil
}

func positiveInt(v string, def int64, name string) (int64, error) {
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("gateway: %s must be a positive integer", name)
	}
	return n, nil
}

func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
