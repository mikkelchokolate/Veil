package generatedconfig

import (
	"path/filepath"
	"testing"
)

func TestGeneratedConfigPathsBuildsKnownGeneratedPaths(t *testing.T) {
	paths := NewPaths("/apply")
	cases := map[string]string{
		paths.Caddyfile(): filepath.Join("/apply", "generated", "caddy", "config.json"),
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

func TestGeneratedConfigPathsUseLiveRootForRuntimeFiles(t *testing.T) {
	paths := NewPathsWithLiveRoot("/staging", "/etc/veil/generated")
	// Generated config artifacts are staged under ApplyRoot.
	if got, want := paths.Generated("hysteria2/server.yaml"), filepath.Join("/staging", "generated", "hysteria2", "server.yaml"); got != want {
		t.Fatalf("Generated = %q, want %q", got, want)
	}
	// Certificate and panel cert paths reference the persistent etc root
	// (parent of the live root) so promoted runtime configs read from the
	// production filesystem, not staging.
	if got, want := paths.CertPath("example.com"), filepath.Join("/etc/veil", "certs", "example.com.crt"); got != want {
		t.Fatalf("CertPath = %q, want %q", got, want)
	}
	if got, want := paths.KeyPath("example.com"), filepath.Join("/etc/veil", "certs", "example.com.key"); got != want {
		t.Fatalf("KeyPath = %q, want %q", got, want)
	}
	if got, want := paths.PanelCertPath(), filepath.Join("/etc/veil", "panel", "tls.crt"); got != want {
		t.Fatalf("PanelCertPath = %q, want %q", got, want)
	}
	if got, want := paths.PanelKeyPath(), filepath.Join("/etc/veil", "panel", "tls.key"); got != want {
		t.Fatalf("PanelKeyPath = %q, want %q", got, want)
	}
}
