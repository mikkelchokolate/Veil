//go:build linux

package caddycert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #1086: every intermediate component of the certificate lookup is opened
// O_NOFOLLOW|O_DIRECTORY relative to the pinned parent — a symlink swapped in
// at any level must fail closed, and the bytes validated are the bytes copied.

func TestFindPairRefusesSymlinkedCertificatesRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	certsRoot := filepath.Join(root, "caddy", "certificates")
	if err := os.MkdirAll(filepath.Dir(certsRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, certsRoot); err != nil {
		t.Fatal(err)
	}
	_, err := FindPair(root, "vpn.example.com")
	if err == nil || errors.Is(err, ErrCertificateNotFound) {
		t.Fatalf("symlinked certificates root must surface an error, got %v", err)
	}
	if !strings.Contains(err.Error(), "read Caddy certificates root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFindPairSkipsSymlinkedIssuerDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	// The outside dir holds a perfectly valid pair — the lookup must still
	// refuse to reach it through the symlinked issuer component.
	writeSelfSignedCertTo(t, outside, "vpn.example.com", time.Hour)
	certsRoot := filepath.Join(root, "caddy", "certificates")
	if err := os.MkdirAll(certsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(certsRoot, "acme-v02.api.letsencrypt.org-directory")); err != nil {
		t.Fatal(err)
	}
	_, err := FindPair(root, "vpn.example.com")
	if !errors.Is(err, ErrCertificateNotFound) {
		t.Fatalf("expected ErrCertificateNotFound past a symlinked issuer, got %v", err)
	}
}

func TestFindPairSkipsSymlinkedDomainDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeSelfSignedCertTo(t, outside, "vpn.example.com", time.Hour)
	domainDirParent := filepath.Join(root, "caddy", "certificates", "acme-v02.api.letsencrypt.org-directory")
	if err := os.MkdirAll(domainDirParent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(domainDirParent, "vpn.example.com")); err != nil {
		t.Fatal(err)
	}
	_, err := FindPair(root, "vpn.example.com")
	if !errors.Is(err, ErrCertificateNotFound) {
		t.Fatalf("expected ErrCertificateNotFound past a symlinked domain dir, got %v", err)
	}
}

func TestFindPairSkipsSymlinkedCertLeaf(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsidePair := writeSelfSignedCertTo(t, outside, "vpn.example.com", time.Hour)
	// Real issuer+domain dirs, but the .crt leaf is a symlink — the openat
	// carries O_NOFOLLOW so it reports ELOOP instead of reading the target.
	pair := writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", "vpn.example.com", time.Hour)
	if err := os.Remove(pair.CertPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePair.CertPath, pair.CertPath); err != nil {
		t.Fatal(err)
	}
	_, err := FindPair(root, "vpn.example.com")
	if !errors.Is(err, ErrCertificateNotFound) {
		t.Fatalf("expected ErrCertificateNotFound past a symlinked cert leaf, got %v", err)
	}
}

// The cert/key match check: a .key that doesn't correspond to the .crt (a
// stale half-rotation or a planted file) must not sync.
func TestFindPairRejectsMismatchedKey(t *testing.T) {
	root := t.TempDir()
	pair := writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", "vpn.example.com", time.Hour)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	otherKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(otherKey)})
	if err := os.WriteFile(pair.KeyPath, otherKeyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = FindPair(root, "vpn.example.com")
	if !errors.Is(err, ErrCertificateNotFound) {
		t.Fatalf("expected ErrCertificateNotFound for a mismatched pair, got %v", err)
	}
}

// Read-once contract: the bytes FindPair validated are returned verbatim on
// the Pair, so the caller copies exactly what was checked — no second open.
func TestFindPairReturnsValidatedBytes(t *testing.T) {
	root := t.TempDir()
	want := writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", "vpn.example.com", time.Hour)
	pair, err := FindPair(root, "vpn.example.com")
	if err != nil {
		t.Fatalf("FindPair: %v", err)
	}
	diskCert, err := os.ReadFile(want.CertPath)
	if err != nil {
		t.Fatal(err)
	}
	diskKey, err := os.ReadFile(want.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(pair.CertPEM) != string(diskCert) || string(pair.KeyPEM) != string(diskKey) {
		t.Fatal("Pair material does not match the on-disk pair")
	}
	if pair.IssuerName != "acme-v02.api.letsencrypt.org-directory" {
		t.Fatalf("IssuerName = %q", pair.IssuerName)
	}
}

func TestFindPairRejectsEscapingDomain(t *testing.T) {
	root := t.TempDir()
	for _, domain := range []string{"../outside", "..", "a/b", "/abs"} {
		if _, err := FindPair(root, domain); err == nil || errors.Is(err, ErrCertificateNotFound) {
			t.Fatalf("domain %q must be rejected as a storage name, got %v", domain, err)
		}
	}
}

// writeSelfSignedCertTo writes a valid self-signed pair directly into dir
// (as <domain>.crt/<domain>.key) and returns its paths.
func writeSelfSignedCertTo(t *testing.T, dir, domain string, lifetime time.Duration) Pair {
	t.Helper()
	tmp := t.TempDir()
	pair := writeSelfSignedCert(t, tmp, "issuer", domain, lifetime)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	certData, err := os.ReadFile(pair.CertPath)
	if err != nil {
		t.Fatal(err)
	}
	keyData, err := os.ReadFile(pair.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, domain+".crt")
	keyPath := filepath.Join(dir, domain+".key")
	if err := os.WriteFile(certPath, certData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyData, 0o600); err != nil {
		t.Fatal(err)
	}
	return Pair{CertPath: certPath, KeyPath: keyPath}
}
