package generatedconfig

import (
	"path/filepath"
	"testing"
)

func TestGeneratedConfigPathsBuildsKnownGeneratedPaths(t *testing.T) {
	paths := NewPaths("/apply")
	cases := map[string]string{
		paths.Caddyfile(): filepath.Join("/apply", "generated", "caddy", "panel.Caddyfile"),
		paths.Hysteria2(): filepath.Join("/apply", "generated", "hysteria2", "server.yaml"),
		paths.Warp():      filepath.Join("/apply", "generated", "sing-box", "warp.json"),
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
	}
}

func TestGeneratedConfigPathsBuildsCertPaths(t *testing.T) {
	paths := NewPaths("/apply")
	if got, want := paths.CertPath("example.com"), filepath.Join("/apply", "certs", "example.com.crt"); got != want {
		t.Fatalf("CertPath = %q, want %q", got, want)
	}
	if got, want := paths.KeyPath("example.com"), filepath.Join("/apply", "certs", "example.com.key"); got != want {
		t.Fatalf("KeyPath = %q, want %q", got, want)
	}
	if got, want := paths.PanelCertPath(), filepath.Join("/apply", "panel", "tls.crt"); got != want {
		t.Fatalf("PanelCertPath = %q, want %q", got, want)
	}
	if got, want := paths.PanelKeyPath(), filepath.Join("/apply", "panel", "tls.key"); got != want {
		t.Fatalf("PanelKeyPath = %q, want %q", got, want)
	}
}
