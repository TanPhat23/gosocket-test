// Command livefixture is a throwaway HTTPS smart-HTTP git server for the live Gate 0C probes.
// It serves one bare repo as /org/repo.git behind Basic auth with a fixed token, using a CA it generates.
// It is test tooling and is never built into the product.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func run(dir string, args ...string) {
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		log.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func out(dir string, args ...string) []byte {
	c := exec.Command("git", args...)
	c.Dir = dir
	b, err := c.Output()
	must(err)
	return b
}

func main() {
	dir := flag.String("dir", "/data", "data directory")
	token := flag.String("token", "", "required Basic auth password")
	host := flag.String("host", "github.com", "certificate DNS name")
	delay := flag.Duration("upload-pack-delay", 0, "delay before answering upload-pack, so a probe can inspect a running worker")
	flag.Parse()
	if *token == "" {
		log.Fatal("-token required")
	}

	bare := filepath.Join(*dir, "org", "repo.git")
	must(os.MkdirAll(filepath.Dir(bare), 0o755))
	run(*dir, "init", "--bare", "-b", "main", bare)
	run(bare, "config", "http.receivepack", "true")
	work := filepath.Join(*dir, "seed")
	run(*dir, "clone", bare, work)
	must(os.WriteFile(filepath.Join(work, "README"), []byte("seed\n"), 0o644))
	run(work, "add", ".")
	run(work, "commit", "-m", "seed")
	run(work, "push", "origin", "HEAD:refs/heads/main")
	seed := strings.TrimSpace(string(out(work, "rev-parse", "HEAD")))

	// Probe branches, each one commit on top of the seed.
	branch := func(name string, files map[string]string, extra func()) {
		run(work, "checkout", "-q", "-B", name, seed)
		for f, body := range files {
			must(os.WriteFile(filepath.Join(work, f), []byte(body), 0o644))
		}
		if extra != nil {
			extra()
		}
		run(work, "add", "-A")
		run(work, "commit", "-q", "-m", name)
		run(work, "push", "-q", "origin", "HEAD:refs/heads/"+name)
	}
	branch("withsub", map[string]string{".gitmodules": "[submodule \"s\"]\n\tpath = s\n\turl = https://example.invalid/s.git\n"},
		func() { run(work, "update-index", "--add", "--cacheinfo", "160000,"+seed+",s") })
	branch("withlfs", map[string]string{".gitattributes": "*.bin filter=lfs diff=lfs merge=lfs -text\n"}, nil)
	branch("lfsptr", map[string]string{"big.bin": "version https://git-lfs.github.com/spec/v1\noid sha256:0000000000000000000000000000000000000000000000000000000000000000\nsize 1\n"}, nil)
	branch("evil", map[string]string{".gitattributes": "* filter=evil\n"}, nil)
	// A second commit on main, so a depth-1 fetch really is shallow.
	branch("main", map[string]string{"SECOND": "second\n"}, nil)

	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "atm-live-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	must(err)
	caCert, _ := x509.ParseCertificate(caDER)
	must(os.WriteFile(filepath.Join(*dir, "ca.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644))

	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: *host},
		DNSNames: []string{*host}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	must(err)

	backendPath := "/usr/lib/git-core/git-http-backend"
	if _, err := os.Stat(backendPath); err != nil {
		backendPath = "/usr/libexec/git-core/git-http-backend" // Alpine
	}
	backend := &cgi.Handler{Path: backendPath,
		Env: []string{"GIT_PROJECT_ROOT=" + *dir, "GIT_HTTP_EXPORT_ALL=1"}}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pw, ok := r.BasicAuth()
		log.Printf("%s %s auth=%v token_ok=%v", r.Method, r.URL.Path, ok, ok && pw == *token)
		if !ok || pw != *token {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if *delay > 0 && r.Method == http.MethodPost {
			time.Sleep(*delay)
		}
		backend.ServeHTTP(w, r)
	})
	ln, err := net.Listen("tcp", ":443")
	must(err)
	srv := &http.Server{Handler: h, TLSConfig: &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}}
	log.Printf("ready")
	must(srv.ServeTLS(ln, "", ""))
}
