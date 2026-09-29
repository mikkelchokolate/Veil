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
	"strings"
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

func TestReadTLSCertReportsInvalidPEM(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	if err := os.WriteFile(certPath, []byte("not a pem block"), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	info := ReadTLSCert(certPath)
	if info.Valid || info.Error != "failed to decode PEM block" {
		t.Fatalf("expected PEM decode error, got %+v", info)
	}
}

func TestReadTLSCertReportsInvalidCertificateBytes(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("invalid der")})
	if err := os.WriteFile(certPath, block, 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	info := ReadTLSCert(certPath)
	if info.Valid || !strings.Contains(info.Error, "parse certificate") {
		t.Fatalf("expected parse certificate error, got %+v", info)
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

// ClassifyManagedCertSource is the honesty contract behind the per-inbound
// TLS status (#1168): self-signed material must never badge as "acme", and
// Caddy's local authority stays "internal".
func TestClassifyManagedCertSource(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if got := ClassifyManagedCertSource(nil); got != TLSCertSourceMissing {
			t.Fatalf("nil cert = %q, want %q", got, TLSCertSourceMissing)
		}
	})

	// makeLeaf issues a leaf signed by parentKey with the given issuer CN; a
	// nil parent produces a self-signed leaf.
	makeLeaf := func(t *testing.T, issuerCN string, selfSigned bool) *x509.Certificate {
		t.Helper()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject:      pkix.Name{CommonName: "leaf.example.com"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(24 * time.Hour),
			DNSNames:     []string{"leaf.example.com"},
		}
		parent := tmpl
		signKey := key
		if !selfSigned {
			parent = &x509.Certificate{Subject: pkix.Name{CommonName: issuerCN}}
			signKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signKey)
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return cert
	}

	t.Run("self-signed", func(t *testing.T) {
		if got := ClassifyManagedCertSource(makeLeaf(t, "", true)); got != TLSCertSourceSelfSigned {
			t.Fatalf("self-signed leaf = %q, want %q", got, TLSCertSourceSelfSigned)
		}
	})
	t.Run("caddy-local", func(t *testing.T) {
		cert := makeLeaf(t, "Caddy Local Authority - ECC Intermediate", false)
		if got := ClassifyManagedCertSource(cert); got != TLSCertSourceInternal {
			t.Fatalf("caddy-local leaf = %q, want %q", got, TLSCertSourceInternal)
		}
	})
	t.Run("ca-issued", func(t *testing.T) {
		cert := makeLeaf(t, "R11", false)
		if got := ClassifyManagedCertSource(cert); got != TLSCertSourceACME {
			t.Fatalf("CA leaf = %q, want %q", got, TLSCertSourceACME)
		}
	})
}
