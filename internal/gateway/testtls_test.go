package gateway

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"
)

type testPKI struct {
	caPEM, certPEM, keyPEM []byte
	pool                   *x509.CertPool
}

var (
	pkiOnce sync.Once
	pkiVal  testPKI
)

// newTestPKI issues a throwaway CA and a leaf for 127.0.0.1 valid from notBefore to notAfter.
func newTestPKI(t testing.TB, notBefore, notAfter time.Time) testPKI {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "gateway"},
		NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(leafKey)
	p := testPKI{
		caPEM:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		keyPEM:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		pool:    x509.NewCertPool(),
	}
	p.pool.AddCert(caCert)
	return p
}

func validPKI(t testing.TB) testPKI {
	pkiOnce.Do(func() { pkiVal = newTestPKI(t, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour)) })
	return pkiVal
}

const testPriceJSON = `{"models":{"gpt-a":{"input_usd_per_mtok":"1","output_usd_per_mtok":"2"},"gpt-b":{"input_usd_per_mtok":1,"output_usd_per_mtok":2.5}}}`

// goodFiles serves the TLS, price and (via ErrNotExist) nothing else a valid configuration points at.
func goodFiles(t testing.TB) func(string) ([]byte, error) {
	pki := validPKI(t)
	return func(p string) ([]byte, error) {
		switch p {
		case "/tls/cert.pem":
			return pki.certPEM, nil
		case "/tls/key.pem":
			return pki.keyPEM, nil
		case "/etc/atm/prices.json":
			return []byte(testPriceJSON), nil
		}
		return nil, errNoFile
	}
}
