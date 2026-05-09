package generatedconfig

import (
	"path/filepath"
	"testing"
)

func TestGeneratedConfigPathsIncludesMieruServerConfig(t *testing.T) {
	paths := NewPaths("/etc/veil")
	want := filepath.Join("/etc/veil", "generated", "mieru", "server_config.json")
	if got := paths.Mieru(); got != want {
		t.Fatalf("Mieru path = %q, want %q", got, want)
	}
}
