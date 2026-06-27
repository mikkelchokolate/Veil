package caddycert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindPairPrefersACMEOverLocal(t *testing.T) {
	root := t.TempDir()
	domain := "vpn.example.com"

	// Local (internal CA) certificate.
	writeSelfSignedCert(t, root, "local", domain, time.Hour)
	// ACME certificate.
	acmePair := writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", domain, time.Hour)

	pair, err := FindPair(root, domain)
	if err != nil {
		t.Fatalf("FindPair: %v", err)
	}
	if pair.CertPath != acmePair.CertPath || pair.KeyPath != acmePair.KeyPath {
		t.Fatalf("expected ACME pair %v, got %v", acmePair, pair)
	}
}

func TestFindPairSkipsExpiredCertificate(t *testing.T) {
	root := t.TempDir()
	domain := "vpn.example.com"

	writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", domain, -time.Hour)

	_, err := FindPair(root, domain)
	if err != ErrCertificateNotFound {
		t.Fatalf("expected ErrCertificateNotFound for expired cert, got %v", err)
	}
}

func TestFindPairRequiresDomainMatch(t *testing.T) {
	root := t.TempDir()
	writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", "other.example.com", time.Hour)

	_, err := FindPair(root, "vpn.example.com")
	if err != ErrCertificateNotFound {
		t.Fatalf("expected ErrCertificateNotFound for mismatched domain, got %v", err)
	}
}

func TestFindPairMissingDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nonexistent")
	_, err := FindPair(root, "vpn.example.com")
	if err != ErrCertificateNotFound {
		t.Fatalf("expected ErrCertificateNotFound for missing directory, got %v", err)
	}
}

func writeSelfSignedCert(t *testing.T, root, issuer, domain string, lifetime time.Duration) Pair {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	now := time.Now()
	cert := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: domain},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(lifetime),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{domain},
	}
	der, err := x509.CreateCertificate(rand.Reader, &cert, &cert, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	dir := filepath.Join(root, "caddy", "certificates", issuer, domain)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	certPath := filepath.Join(dir, domain+".crt")
	keyPath := filepath.Join(dir, domain+".key")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return Pair{CertPath: certPath, KeyPath: keyPath}
}
