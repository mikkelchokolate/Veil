package privileged

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/caddycert"
)

// Regression for #1006: the Caddy cert tree lives under the veil-proxy-owned
// StateDirectory, so its leaf files are attacker-replaceable. A .crt or .key
// swapped for a symlink must be rejected — following it copies an arbitrary
// root-owned file into the veil-proxy-readable output directory.
func TestRunSyncCaddyCertRejectsSymlinkedSourceMaterial(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test")
	}
	original := findCaddyCertPair
	defer func() { findCaddyCertPair = original }()

	setup := func(t *testing.T, linkCert bool) (string, string) {
		root := t.TempDir()
		secret := filepath.Join(root, "root-secret")
		if err := os.WriteFile(secret, []byte("root-only-secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		certDir := filepath.Join(root, "caddy", "certs")
		if err := os.MkdirAll(certDir, 0o700); err != nil {
			t.Fatal(err)
		}
		certPath := filepath.Join(certDir, "example.com.crt")
		keyPath := filepath.Join(certDir, "example.com.key")
		if linkCert {
			if err := os.Symlink(secret, certPath); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if err := os.WriteFile(keyPath, []byte("key-data"), 0o600); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.WriteFile(certPath, []byte("cert-data"), 0o600); err != nil {
				t.Fatal(err)
			}
			// The key is the high-value target: a swapped .key link would leak
			// any root-readable file to the veil-proxy group.
			if err := os.Symlink(secret, keyPath); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
		}
		findCaddyCertPair = func(_, domain string) (caddycert.Pair, error) {
			if domain != "example.com" {
				return caddycert.Pair{}, caddycert.ErrCertificateNotFound
			}
			return caddycert.Pair{CertPath: certPath, KeyPath: keyPath}, nil
		}
		return root, secret
	}

	for _, linkCert := range []bool{false, true} {
		name := "key"
		if linkCert {
			name = "cert"
		}
		t.Run(name+" is a symlink", func(t *testing.T) {
			root, secret := setup(t, linkCert)
			outDir := filepath.Join(root, "out")
			_, err := runSyncCaddyCert(context.Background(), SyncCaddyCertRequest{
				Domain: "example.com", OutDir: outDir,
			}, ProductionConfig{CertDirs: []string{outDir}})
			if err == nil {
				t.Fatal("sync followed a symlinked source file")
			}
			if !strings.Contains(err.Error(), "Caddy") {
				t.Fatalf("unexpected error: %v", err)
			}
			// Nothing from the symlink target may land in the output dir.
			for _, name := range []string{"example.com.crt", "example.com.key"} {
				out := filepath.Join(outDir, name)
				if body, readErr := os.ReadFile(out); readErr == nil {
					t.Fatalf("symlink target content was copied to %s: %q", out, body)
				}
			}
			if _, err := os.Stat(secret); err != nil {
				t.Fatalf("secret source file disappeared: %v", err)
			}
		})
	}
}
