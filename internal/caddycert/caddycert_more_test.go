package caddycert

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindPairMore(t *testing.T) {
	tests := []struct {
		name              string
		useDefaultDataDir bool
		domain            string
		setup             func(t *testing.T) (root string, want Pair, wantErr func(error) bool)
	}{
		{
			name:              "empty data dir falls back to defaultDataDir",
			useDefaultDataDir: true,
			domain:            "vpn.example.com",
			setup: func(t *testing.T) (string, Pair, func(error) bool) {
				root := t.TempDir()
				pair := writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", "vpn.example.com", time.Hour)
				old := defaultDataDir
				defaultDataDir = root
				t.Cleanup(func() { defaultDataDir = old })
				return root, pair, nil
			},
		},
		{
			name:              "empty domain returns error",
			useDefaultDataDir: false,
			domain:            "",
			setup: func(t *testing.T) (string, Pair, func(error) bool) {
				root := t.TempDir()
				writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", "vpn.example.com", time.Hour)
				return root, Pair{}, func(err error) bool {
					return err != nil && err.Error() == "domain is required"
				}
			},
		},
		{
			name:              "non-directory certificates root returns wrapped error",
			useDefaultDataDir: false,
			domain:            "vpn.example.com",
			setup: func(t *testing.T) (string, Pair, func(error) bool) {
				root := t.TempDir()
				certsRoot := filepath.Join(root, "caddy", "certificates")
				if err := os.MkdirAll(filepath.Dir(certsRoot), 0o700); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(certsRoot, []byte("not a directory"), 0o600); err != nil {
					t.Fatalf("write file: %v", err)
				}
				return root, Pair{}, func(err error) bool {
					return err != nil && !errors.Is(err, ErrCertificateNotFound)
				}
			},
		},
		{
			name:              "non-directory issuer entry is skipped",
			useDefaultDataDir: false,
			domain:            "vpn.example.com",
			setup: func(t *testing.T) (string, Pair, func(error) bool) {
				root := t.TempDir()
				certsRoot := filepath.Join(root, "caddy", "certificates")
				if err := os.MkdirAll(certsRoot, 0o700); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(certsRoot, "readme.txt"), []byte("ignore me"), 0o600); err != nil {
					t.Fatalf("write file: %v", err)
				}
				pair := writeSelfSignedCert(t, root, "local", "vpn.example.com", time.Hour)
				return root, pair, nil
			},
		},
		{
			name:              "missing key file skips certificate",
			useDefaultDataDir: false,
			domain:            "vpn.example.com",
			setup: func(t *testing.T) (string, Pair, func(error) bool) {
				root := t.TempDir()
				pair := writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", "vpn.example.com", time.Hour)
				if err := os.Remove(pair.KeyPath); err != nil {
					t.Fatalf("remove key: %v", err)
				}
				return root, Pair{}, func(err error) bool { return errors.Is(err, ErrCertificateNotFound) }
			},
		},
		{
			name:              "non-ACME non-local issuer is accepted",
			useDefaultDataDir: false,
			domain:            "vpn.example.com",
			setup: func(t *testing.T) (string, Pair, func(error) bool) {
				root := t.TempDir()
				pair := writeSelfSignedCert(t, root, "my-custom-ca", "vpn.example.com", time.Hour)
				return root, pair, nil
			},
		},
		{
			name:              "ACME tie broken by newer certificate mod time",
			useDefaultDataDir: false,
			domain:            "vpn.example.com",
			setup: func(t *testing.T) (string, Pair, func(error) bool) {
				root := t.TempDir()
				older := writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", "vpn.example.com", time.Hour)
				newer := writeSelfSignedCert(t, root, "acme-zerossl.acme.directory", "vpn.example.com", time.Hour)
				// Ensure the second certificate has a materially later ModTime.
				future := time.Now().Add(24 * time.Hour)
				if err := os.Chtimes(newer.CertPath, future, future); err != nil {
					t.Fatalf("chtimes: %v", err)
				}
				_ = older
				return root, newer, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, want, wantErr := tt.setup(t)
			dataDir := root
			if tt.useDefaultDataDir {
				dataDir = ""
			}
			pair, err := FindPair(dataDir, tt.domain)
			if wantErr != nil {
				if !wantErr(err) {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("FindPair: %v", err)
			}
			if pair.CertPath != want.CertPath || pair.KeyPath != want.KeyPath {
				t.Fatalf("expected pair %+v, got %+v", want, pair)
			}
		})
	}
}

func TestIssuerScore(t *testing.T) {
	tests := []struct {
		issuer string
		want   int
	}{
		{"local", 0},
		{"acme-v02.api.letsencrypt.org-directory", 2},
		{"acme-zerossl.acme.directory", 2},
		{"my-custom-ca", 1},
		{"", 1},
	}
	for _, tt := range tests {
		t.Run(tt.issuer, func(t *testing.T) {
			if got := issuerScore(tt.issuer); got != tt.want {
				t.Fatalf("issuerScore(%q) = %d, want %d", tt.issuer, got, tt.want)
			}
		})
	}
}

func TestIsValidCertificate(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T) (certPath, domain string)
		want    bool
	}{
		{
			name: "valid matching certificate",
			prepare: func(t *testing.T) (string, string) {
				root := t.TempDir()
				pair := writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", "vpn.example.com", time.Hour)
				return pair.CertPath, "vpn.example.com"
			},
			want: true,
		},
		{
			name: "expired certificate",
			prepare: func(t *testing.T) (string, string) {
				root := t.TempDir()
				pair := writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", "vpn.example.com", -time.Hour)
				return pair.CertPath, "vpn.example.com"
			},
			want: false,
		},
		{
			name: "not-yet-valid certificate",
			prepare: func(t *testing.T) (string, string) {
				root := t.TempDir()
				certPath := filepath.Join(root, "cert.crt")
				now := time.Now()
				certPEM := makeSelfSignedCertPEM(t, "vpn.example.com", now.Add(time.Hour), now.Add(24*time.Hour), []string{"vpn.example.com"})
				if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
					t.Fatalf("write cert: %v", err)
				}
				return certPath, "vpn.example.com"
			},
			want: false,
		},
		{
			name: "SAN mismatch",
			prepare: func(t *testing.T) (string, string) {
				root := t.TempDir()
				pair := writeSelfSignedCert(t, root, "acme-v02.api.letsencrypt.org-directory", "other.example.com", time.Hour)
				return pair.CertPath, "vpn.example.com"
			},
			want: false,
		},
		{
			name: "unreadable certificate path",
			prepare: func(t *testing.T) (string, string) {
				root := t.TempDir()
				certPath := filepath.Join(root, "cert.crt")
				if err := os.Mkdir(certPath, 0o700); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				return certPath, "vpn.example.com"
			},
			want: false,
		},
		{
			name: "non-PEM file",
			prepare: func(t *testing.T) (string, string) {
				root := t.TempDir()
				certPath := filepath.Join(root, "cert.crt")
				if err := os.WriteFile(certPath, []byte("this is not a PEM block"), 0o600); err != nil {
					t.Fatalf("write cert: %v", err)
				}
				return certPath, "vpn.example.com"
			},
			want: false,
		},
		{
			name: "PEM block with invalid DER bytes",
			prepare: func(t *testing.T) (string, string) {
				root := t.TempDir()
				certPath := filepath.Join(root, "cert.crt")
				badPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not valid DER")})
				if err := os.WriteFile(certPath, badPEM, 0o600); err != nil {
					t.Fatalf("write cert: %v", err)
				}
				return certPath, "vpn.example.com"
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certPath, domain := tt.prepare(t)
			if got := isValidCertificate(certPath, domain); got != tt.want {
				t.Fatalf("isValidCertificate(%q, %q) = %v, want %v", certPath, domain, got, tt.want)
			}
		})
	}
}

// makeSelfSignedCertPEM creates a self-signed certificate for the given DNS
// names and returns its PEM encoding.
func makeSelfSignedCertPEM(t *testing.T, commonName string, notBefore, notAfter time.Time, dnsNames []string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	cert := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
	}
	der, err := x509.CreateCertificate(rand.Reader, &cert, &cert, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
