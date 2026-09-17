package api

import (
	"os"
	"path/filepath"
	"testing"
)

// Phase-2 cert sync reads the Caddy-managed domain from the config artifact
// being made live: a generated hysteria2 config whose tls.cert points into the
// certs directory gets its domain synced. This is the rendered equivalent of
// the old candidate-inbound lookup (including the settings.Domain fallback,
// which the renderer bakes into the cert path).
func TestHysteria2CertDomainsFromConfigsFindsManagedDomain(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "hysteria2", "edge.yaml")
	writeHysteria2YAML(t, cfg, filepath.Join(root, "certs", "hy.example.com.crt"))
	domains := hysteria2CertDomainsFromConfigs([]string{cfg})
	if len(domains) != 1 || domains[0] != "hy.example.com" {
		t.Fatalf("expected [hy.example.com], got %v", domains)
	}
}

// A hysteria2 config that serves the panel cert must NOT trigger a Caddy cert
// sync: the panel cert is not Caddy-managed (regression: syncing blocked apply
// on cert polling and aborted before the service restart).
func TestHysteria2CertDomainsFromConfigsSkipsPanelCert(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "hysteria2", "edge.yaml")
	writeHysteria2YAML(t, cfg, filepath.Join(root, "panel", "tls.crt"))
	if domains := hysteria2CertDomainsFromConfigs([]string{cfg}); len(domains) != 0 {
		t.Fatalf("expected no cert-sync domains for panel-cert config, got %v", domains)
	}
}

// Two inbounds sharing a domain produce one sync request; unrelated files are
// ignored.
func TestHysteria2CertDomainsFromConfigsDedupes(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "hysteria2", "a.yaml")
	b := filepath.Join(root, "hysteria2", "b.yaml")
	other := filepath.Join(root, "olcrtc", "c.yaml")
	writeHysteria2YAML(t, a, filepath.Join(root, "certs", "shared.example.com.crt"))
	writeHysteria2YAML(t, b, filepath.Join(root, "certs", "shared.example.com.crt"))
	writeHysteria2YAML(t, other, filepath.Join(root, "certs", "ignored.example.com.crt"))
	domains := hysteria2CertDomainsFromConfigs([]string{a, b, other})
	if len(domains) != 1 || domains[0] != "shared.example.com" {
		t.Fatalf("expected [shared.example.com], got %v", domains)
	}
}

func writeHysteria2YAML(t *testing.T, path, certPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "listen: ':443'\ntls:\n  cert: " + certPath + "\n  key: " + certPath + ".key\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
