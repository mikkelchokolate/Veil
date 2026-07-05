package generatedconfig

import (
	"path/filepath"
	"testing"
)

func TestGeneratedConfigPathsBuildsKnownGeneratedPaths(t *testing.T) {
	paths := NewPaths("/apply")
	cases := map[string]string{
		paths.CaddyJSON(): filepath.Join("/apply", "generated", "caddy", "config.json"),
		paths.Hysteria2(): filepath.Join("/apply", "generated", "hysteria2", "server.yaml"),
		paths.Warp():      filepath.Join("/apply", "generated", "sing-box", "warp.json"),
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
	}
}
