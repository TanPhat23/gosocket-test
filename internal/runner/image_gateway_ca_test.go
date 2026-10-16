package runner_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"io"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/image"

	"agent-task-manager/internal/runner"
)

func testCAPEM(t *testing.T, cn string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kd, _ := x509.MarshalECPrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kd})
}

func TestGatewayCA_RejectsAnythingButCertificates(t *testing.T) {
	cert, key := testCAPEM(t, "ca")
	withHeader := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Headers: map[string]string{"k": "v"}, Bytes: []byte("x")})
	bad := map[string][]byte{
		"empty":            nil,
		"whitespace":       []byte("  \n"),
		"garbage":          []byte("not pem"),
		"private key only": key,
		"cert plus key":    append(append([]byte{}, cert...), key...),
		"trailing junk":    append(append([]byte{}, cert...), []byte("junk")...),
		"bad cert bytes":   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not der")}),
		"pem headers":      withHeader,
	}
	for name, b := range bad {
		if _, err := runner.NewRuntimeImageBuilderWithGatewayCA(nil, runner.FixtureCatalogForTest(), b); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "PRIVATE") {
			t.Errorf("%s: error echoes key material", name)
		}
	}
	if _, err := runner.NewRuntimeImageBuilderWithGatewayCA(nil, runner.FixtureCatalogForTest(), cert); err != nil {
		t.Errorf("a valid CA certificate was rejected: %v", err)
	}
}

type builtContext struct {
	tag   string
	files map[string][]byte
}

func buildCtxWith(t *testing.T, b func(cli *mockDockerClientForBuilder) runner.RuntimeImageBuilder) builtContext {
	t.Helper()
	cli := &mockDockerClientForBuilder{
		imageListFunc: func(context.Context, image.ListOptions) ([]image.Summary, error) { return nil, nil },
	}
	profile := &runner.ResolvedRuntimeProfile{
		Platform: "linux/amd64", BaseImageRef: runner.FixtureBaseRefForTest, TemplateRev: "v1",
		OpenCodeDigest: runner.DefaultOpenCodeDigest, Runtimes: map[string]string{"go": "1.27.2"},
	}
	tag, err := b(cli).BuildOrGetImage(context.Background(), profile)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	cli.mu.Lock()
	defer cli.mu.Unlock()
	if len(cli.buildTarPayloads) == 0 {
		t.Fatal("no build context sent")
	}
	out := builtContext{tag: tag, files: map[string][]byte{}}
	tr := tar.NewReader(bytes.NewReader(cli.buildTarPayloads[0]))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		out.files[h.Name], _ = io.ReadAll(tr)
	}
	return out
}

func TestGatewayCA_IsBakedIntoImageAndIdentity(t *testing.T) {
	caA, _ := testCAPEM(t, "ca-a")
	caB, _ := testCAPEM(t, "ca-b")
	with := func(ca []byte) func(*mockDockerClientForBuilder) runner.RuntimeImageBuilder {
		return func(cli *mockDockerClientForBuilder) runner.RuntimeImageBuilder {
			b, err := runner.NewRuntimeImageBuilderWithGatewayCA(cli, runner.FixtureCatalogForTest(), ca)
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
	}
	none := buildCtxWith(t, func(cli *mockDockerClientForBuilder) runner.RuntimeImageBuilder {
		return newTestBuilder(cli, runner.FixtureCatalogForTest())
	})
	a := buildCtxWith(t, with(caA))
	b := buildCtxWith(t, with(caB))

	if _, ok := none.files["atm-gateway-ca.crt"]; ok {
		t.Error("a builder without a gateway CA must not put one in the build context")
	}
	if strings.Contains(string(none.files["Dockerfile"]), "NODE_EXTRA_CA_CERTS") {
		t.Error("a builder without a gateway CA must not set NODE_EXTRA_CA_CERTS")
	}
	if !bytes.Equal(a.files["atm-gateway-ca.crt"], caA) {
		t.Error("the build context must carry exactly the supplied CA certificate")
	}
	df := string(a.files["Dockerfile"])
	sum := sha256.Sum256(caA)
	for _, want := range []string{
		"COPY --chmod=0444 atm-gateway-ca.crt /usr/local/share/atm/gateway-ca.crt",
		"ENV NODE_EXTRA_CA_CERTS=/usr/local/share/atm/gateway-ca.crt",
		`LABEL io.atm.gateway-ca.sha256="` + hex.EncodeToString(sum[:]) + `"`,
	} {
		if !strings.Contains(df, want) {
			t.Errorf("Dockerfile missing %q", want)
		}
	}
	if strings.Index(df, "NODE_EXTRA_CA_CERTS") > strings.Index(df, "USER opencode") {
		t.Error("the CA must be installed before the unprivileged USER switch")
	}
	if a.tag == none.tag || a.tag == b.tag {
		t.Errorf("the image tag must change with the CA: none=%s a=%s b=%s", none.tag, a.tag, b.tag)
	}
	for name, f := range a.files {
		if bytes.Contains(f, []byte("PRIVATE KEY")) {
			t.Errorf("%s contains private key material", name)
		}
	}
}
