package runner

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"agent-task-manager/internal/gateway"
	"agent-task-manager/internal/models"
)

const testVerifyImage = "registry.example/atm-helper@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// selfSignedPEM issues one self-signed certificate that serves as both the gateway leaf and its CA.
func selfSignedPEM(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "gateway"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("172.29.0.2")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd})
}

type runtimeEnv struct {
	env   map[string]string
	files map[string][]byte
	gw    *gateway.Service
}

func (r *runtimeEnv) getenv(k string) string { return r.env[k] }
func (r *runtimeEnv) readFile(p string) ([]byte, error) {
	if b, ok := r.files[p]; ok {
		return b, nil
	}
	return nil, errors.New("no such file")
}

func newRuntimeEnv(t *testing.T) *runtimeEnv {
	t.Helper()
	cert, key := selfSignedPEM(t)
	r := &runtimeEnv{
		files: map[string][]byte{
			"/gw.pem": cert, "/gw.key": key, "/ca.pem": cert, "/provider.key": []byte("sk-provider-secret"),
			"/prices.json": []byte(`{"models":{"m1":{"input_usd_per_mtok":"1","output_usd_per_mtok":"2"}}}`),
		},
		env: map[string]string{
			"ATM_GATEWAY_ENABLED": "true", "ATM_GATEWAY_LISTEN_ADDR": "172.29.0.2:8082", "ATM_GATEWAY_MODELS": "m1",
			"ATM_GATEWAY_UPSTREAM_URL":      "https://upstream.example/v1/chat/completions",
			"ATM_GATEWAY_PROVIDER_KEY_FILE": "/provider.key", "ATM_GATEWAY_TLS_CERT_FILE": "/gw.pem", "ATM_GATEWAY_TLS_KEY_FILE": "/gw.key",
			"ATM_GATEWAY_PRICE_FILE": "/prices.json", "ATM_GATEWAY_QUOTA_USD": "1.00",
			"ATM_GATEWAY_TOKEN_SECRET":   strings.Repeat("s", 40),
			"ATM_GATEWAY_QUOTA_REQUESTS": "100", "ATM_GATEWAY_QUOTA_TOKENS": "100000", "ATM_GATEWAY_MAX_OUTPUT_TOKENS": "100",
			"ATM_GATEWAY_MAX_BODY_BYTES": "65536", "ATM_GATEWAY_IDLE_TIMEOUT": "3s",
			"ATM_GATEWAY_FIREWALL_ATTESTATION": gateway.FirewallAttestation,

			"ATM_PROJECT_RUNTIME_ENABLED": "true", "ATM_SANDBOX_GATEWAY_URL": "https://172.29.0.2:8082/v1",
			"ATM_GATEWAY_CA_FILE": "/ca.pem", "ATM_SANDBOX_CONTEXT_TOKENS": "128000",
			"ATM_SANDBOX_PROXY_URL": "http://172.28.200.10:3128", "ATM_EGRESS_VERIFY_IMAGE": testVerifyImage,
			"ATM_EGRESS_CANARIES": "192.168.1.10:80, 10.0.0.1:22",
		},
	}
	gw, err := gateway.ServiceFromEnv(r.getenv, r.readFile, func(string, ...any) {})
	if err != nil || gw == nil {
		t.Fatalf("gateway fixture: %v", err)
	}
	r.gw = gw
	return r
}

func (r *runtimeEnv) build(cli *gwClient) (*ProjectRuntime, error) {
	return ProjectRuntimeFromEnv(r.getenv, r.readFile, cli, NewDefaultRuntimeCatalog(), r.gw, 15*time.Minute)
}

func TestProjectRuntimeFromEnv_OffByDefault(t *testing.T) {
	r := newRuntimeEnv(t)
	for _, v := range []string{"", "false"} {
		r.env["ATM_PROJECT_RUNTIME_ENABLED"] = v
		if rt, err := r.build(newGWClient()); rt != nil || err != nil {
			t.Fatalf("%q: want nil,nil got %v,%v", v, rt, err)
		}
	}
	r.env["ATM_PROJECT_RUNTIME_ENABLED"] = "yes"
	if _, err := r.build(newGWClient()); err == nil {
		t.Error("an invalid flag must be an error")
	}
}

func TestProjectRuntimeFromEnv_FailsClosed(t *testing.T) {
	cases := map[string]func(r *runtimeEnv){
		"gateway url not https": func(r *runtimeEnv) { r.env["ATM_SANDBOX_GATEWAY_URL"] = "http://172.29.0.2:8082/v1" },
		"gateway url missing":   func(r *runtimeEnv) { delete(r.env, "ATM_SANDBOX_GATEWAY_URL") },
		"CA file missing":       func(r *runtimeEnv) { r.env["ATM_GATEWAY_CA_FILE"] = "/nope" },
		"CA is a private key":   func(r *runtimeEnv) { r.files["/ca.pem"] = r.files["/gw.key"] },
		"context tokens unset":  func(r *runtimeEnv) { delete(r.env, "ATM_SANDBOX_CONTEXT_TOKENS") },
		"context tokens zero":   func(r *runtimeEnv) { r.env["ATM_SANDBOX_CONTEXT_TOKENS"] = "0" },
		"proxy not an ip":       func(r *runtimeEnv) { r.env["ATM_SANDBOX_PROXY_URL"] = "http://proxy.local:3128" },
		"verify image unpinned": func(r *runtimeEnv) { r.env["ATM_EGRESS_VERIFY_IMAGE"] = "alpine:latest" },
		"no canaries":           func(r *runtimeEnv) { r.env["ATM_EGRESS_CANARIES"] = " , " },
		"canary is a hostname":  func(r *runtimeEnv) { r.env["ATM_EGRESS_CANARIES"] = "example.com:80" },
		"canary has no port":    func(r *runtimeEnv) { r.env["ATM_EGRESS_CANARIES"] = "10.0.0.1" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRuntimeEnv(t)
			mutate(r)
			rt, err := r.build(newGWClient())
			if err == nil || rt != nil {
				t.Fatalf("want error and no runtime, got %v, %v", rt, err)
			}
			if strings.Contains(err.Error(), "sk-provider-secret") {
				t.Error("error leaked a secret")
			}
		})
	}
	r := newRuntimeEnv(t)
	if _, err := ProjectRuntimeFromEnv(r.getenv, r.readFile, nil, NewDefaultRuntimeCatalog(), r.gw, 15*time.Minute); err == nil {
		t.Error("a nil Docker client must be refused")
	}
	if _, err := ProjectRuntimeFromEnv(r.getenv, r.readFile, newGWClient(), NewDefaultRuntimeCatalog(), nil, 15*time.Minute); err == nil {
		t.Error("a disabled gateway must be refused")
	}
}

// The quarantine was lifted: the assembled manager's public method provisions (firewall proof first).
func TestProjectRuntimeFromEnv_BuildsAndProvisions(t *testing.T) {
	r := newRuntimeEnv(t)
	cli := newGWClient()
	rt, err := r.build(cli)
	if err != nil || rt == nil || rt.Manager == nil || rt.Builder == nil {
		t.Fatalf("want a runtime, got %v, %v", rt, err)
	}
	// The container's agent service is probed before the route is handed out; the mock has nothing to answer it.
	mgr := rt.Manager.(*projectSandboxManager)
	if mgr.readyProbe == nil {
		t.Fatal("the assembled manager must wait for the agent service to be ready")
	}
	mgr.readyProbe = func(context.Context, string, string) error { return nil }
	_, err = rt.Manager.EnsureProjectContainer(context.Background(), &models.Project{ID: "p1"}, "img")
	if err != nil {
		t.Fatalf("want provisioning to succeed, got %v", err)
	}
	if len(cli.CreateCalls) != 2 {
		t.Errorf("want probe + project container, got %d creates", len(cli.CreateCalls))
	}
}

// The assembled manager's (draft) provisioning path proves the firewall on the project network first,
// hands the sandbox a TLS gateway URL and never lets the token into the environment.
func TestProjectRuntimeFromEnv_DraftProvisionsOnlyAfterFirewallProof(t *testing.T) {
	r := newRuntimeEnv(t)
	cli := newGWClient()
	rt, err := r.build(cli)
	if err != nil {
		t.Fatal(err)
	}
	m := rt.Manager.(*projectSandboxManager)
	m.readyProbe = func(context.Context, string, string) error { return nil }
	if _, err := m.ensureProjectContainer(context.Background(), &models.Project{ID: "p1"}, "img"); err != nil {
		t.Fatal(err)
	}
	// Create calls: the egress probe first (joins atm-net-p1), then the project container.
	if len(cli.CreateCalls) != 2 {
		t.Fatalf("want probe + project container, got %d creates", len(cli.CreateCalls))
	}
	probe := cli.CreateCalls[0]
	if string(probe.HostConfig.NetworkMode) != "atm-net-p1" || probe.Config.Image != testVerifyImage {
		t.Errorf("probe must run pinned on the project's network: %v %q", probe.HostConfig.NetworkMode, probe.Config.Image)
	}
	args := strings.Join(probe.Config.Cmd, " ")
	if !strings.Contains(args, "172.28.200.10:3128") || !strings.Contains(args, "192.168.1.10:80") || !strings.Contains(args, "10.0.0.1:22") {
		t.Errorf("probe must check the proxy control and every canary: %q", args)
	}
	env := strings.Join(cli.CreateCalls[1].Config.Env, " ")
	if !strings.Contains(env, "ATM_LLM_GATEWAY_URL=https://172.29.0.2:8082/v1") {
		t.Errorf("sandbox must be pointed at the https gateway: %q", env)
	}
	if strings.Contains(env, "sk-provider-secret") {
		t.Error("provider key must never reach the sandbox env")
	}
}

func TestProjectSandbox_FirewallProofFailureCreatesNothing(t *testing.T) {
	cli := newGWClient()
	m := gwManager(cli, &fakeBroker{})
	boom := errors.New("egress open")
	var gotNet string
	m.egressVerify = func(_ context.Context, network string) error { gotNet = network; return boom }
	_, err := m.ensureProjectContainer(context.Background(), &models.Project{ID: "p1"}, "img")
	if !errors.Is(err, boom) {
		t.Fatalf("want the verification error, got %v", err)
	}
	if gotNet != "atm-net-p1" {
		t.Errorf("verified network = %q", gotNet)
	}
	if len(cli.CreateCalls) != 0 || len(cli.cmds) != 0 {
		t.Error("no container may be created and no secret delivered when the firewall is unproven")
	}
}
