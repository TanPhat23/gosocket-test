package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// FirewallAttestation is the value ATM_GATEWAY_FIREWALL_ATTESTATION holds when the host operator has
// restricted the sandbox bridges to reaching this listener and nothing else. The gateway listens on TLS
// only, so the capability token no longer crosses the bridge in cleartext, but a sandbox that can reach
// other host services is still a risk. ATM never changes firewall rules. Without the attestation the
// gateway still starts but logs a warning (user decision, 2026-10-15).
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
	// TLS is the listener's TLS configuration. Run refuses to serve without it: plain HTTP is never offered.
	TLS *tls.Config
}

// ServiceFromEnv builds the gateway from ATM_GATEWAY_* settings. It returns (nil, nil) when the gateway
// is not enabled, and an error, never a partially configured service, for anything unsafe or missing,
// except the firewall attestation, whose absence only logs a warning.
// Errors name the offending setting and never include secret values.
func ServiceFromEnv(getenv func(string) string, readFile func(string) ([]byte, error), logf func(string, ...any)) (*Service, error) {
	switch getenv("ATM_GATEWAY_ENABLED") {
	case "", "false":
		return nil, nil
	case "true":
	default:
		return nil, errors.New("gateway: ATM_GATEWAY_ENABLED must be \"true\" or \"false\"")
	}
	if logf == nil {
		logf = log.Printf
	}
	if getenv("ATM_GATEWAY_FIREWALL_ATTESTATION") != FirewallAttestation {
		logf("gateway: WARNING: no firewall attestation (ATM_GATEWAY_FIREWALL_ATTESTATION=%s). Starting anyway; sandboxes may reach other host services. Restrict sandbox bridges to the gateway listener to remove this risk.", FirewallAttestation)
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

	tlsCfg, err := loadTLS(getenv("ATM_GATEWAY_TLS_CERT_FILE"), getenv("ATM_GATEWAY_TLS_KEY_FILE"), readFile, time.Now())
	if err != nil {
		return nil, err
	}
	pricing, err := loadPricing(getenv("ATM_GATEWAY_PRICE_FILE"), models, readFile)
	if err != nil {
		return nil, err
	}
	if getenv("ATM_GATEWAY_QUOTA_USD") == "" {
		return nil, errors.New("gateway: ATM_GATEWAY_QUOTA_USD is required (spend cap per sandbox lease, e.g. 5.00)")
	}
	quotaMicro, err := ParseUSD(getenv("ATM_GATEWAY_QUOTA_USD"))
	if err != nil {
		return nil, errors.New("gateway: ATM_GATEWAY_QUOTA_USD must be a positive dollar amount with at most 6 decimals")
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

	ledger := NewLedger(Quota{MaxRequests: limits["ATM_GATEWAY_QUOTA_REQUESTS"], MaxTokens: limits["ATM_GATEWAY_QUOTA_TOKENS"], MaxMicroUSD: quotaMicro})
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
		Pricing:         pricing,
		Logf:            logf,
	})
	if err != nil {
		return nil, err
	}
	return &Service{Addr: addr, Handler: handler, Issuer: issuer, Ledger: ledger, TLS: tlsCfg}, nil
}

// Listen binds the configured address.
func (s *Service) Listen() (net.Listener, error) { return net.Listen("tcp", s.Addr) }

// Run serves on ln until ctx is cancelled, then shuts down gracefully. It returns nil on a clean shutdown.
func (s *Service) Run(ctx context.Context, ln net.Listener) error {
	if s.TLS == nil {
		return errors.New("gateway: refusing to serve without TLS")
	}
	ln = tls.NewListener(ln, s.TLS)
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

// loadTLS builds the listener's TLS config from a PEM certificate chain and key. TLS 1.3 only. The
// leaf must be currently valid and the key must match it. Errors never echo file content.
func loadTLS(certFile, keyFile string, readFile func(string) ([]byte, error), now time.Time) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		return nil, errors.New("gateway: ATM_GATEWAY_TLS_CERT_FILE and ATM_GATEWAY_TLS_KEY_FILE are required (the gateway never serves plain HTTP)")
	}
	certPEM, err := readFile(certFile)
	if err != nil {
		return nil, errors.New("gateway: ATM_GATEWAY_TLS_CERT_FILE is unreadable")
	}
	keyPEM, err := readFile(keyFile)
	if err != nil {
		return nil, errors.New("gateway: ATM_GATEWAY_TLS_KEY_FILE is unreadable")
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errors.New("gateway: TLS certificate and key are invalid or do not match")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil || now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
		return nil, errors.New("gateway: TLS certificate is not currently valid")
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}, nil
}

func loadPricing(path string, models []string, readFile func(string) ([]byte, error)) (Pricing, error) {
	if path == "" {
		return nil, errors.New("gateway: ATM_GATEWAY_PRICE_FILE is required (every allowed model needs a price)")
	}
	raw, err := readFile(path)
	if err != nil {
		return nil, errors.New("gateway: ATM_GATEWAY_PRICE_FILE is unreadable")
	}
	return ParsePricing(raw, models)
}

// listenAddr requires an explicit, non-wildcard host and a real port: a wildcard bind would expose
// the listener on every interface, not just the sandbox-facing one.
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
