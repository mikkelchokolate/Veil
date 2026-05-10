package runtime

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadTLSCertReportsMissingInput(t *testing.T) {
	for _, path := range []string{"", "/nonexistent/cert.pem"} {
		t.Run(path, func(t *testing.T) {
			info := ReadTLSCert(path)
			if info.Valid {
				t.Fatalf("expected invalid certificate info for %q", path)
			}
			if info.Error == "" {
				t.Fatalf("expected error for %q", path)
			}
		})
	}
}

func TestReadTLSCertParsesValidCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	if err := os.WriteFile(certPath, generateRuntimeTLSCert(t), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	info := ReadTLSCert(certPath)
	if !info.Valid {
		t.Fatalf("expected valid cert, got error: %s", info.Error)
	}
	if info.Path != certPath {
		t.Fatalf("path = %q, want %q", info.Path, certPath)
	}
	if info.DaysRemaining <= 0 {
		t.Fatalf("days remaining = %d, want positive", info.DaysRemaining)
	}
	if len(info.DNSNames) != 1 || info.DNSNames[0] != "test.example.com" {
		t.Fatalf("dns names = %+v", info.DNSNames)
	}
}

func generateRuntimeTLSCert(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example.com"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		DNSNames:     []string{"test.example.com"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}
