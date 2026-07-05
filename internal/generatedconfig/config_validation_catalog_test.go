package generatedconfig

import "testing"

func TestConfigValidationCatalogMatchesKnownGeneratedConfigs(t *testing.T) {
	catalog := NewConfigValidationCatalog()
	cases := []struct {
		path string
		name string
		cmd  []string
	}{
		// Only protocols with a working standalone checker have a validation command.
		{"/etc/veil/generated/caddy/config.json", "caddy", []string{"caddy", "validate", "--config", "/etc/veil/generated/caddy/config.json"}},
		{"/etc/veil/generated/sing-box/warp.json", "warp", []string{"sing-box", "check", "-c", "/etc/veil/generated/sing-box/warp.json"}},
	}
	for _, tc := range cases {
		validation, ok := catalog.Match(tc.path)
		if !ok || validation.Name != tc.name {
			t.Fatalf("Match(%q) = %+v %v", tc.path, validation, ok)
		}
		if len(validation.Command) != len(tc.cmd) {
			t.Fatalf("command = %+v", validation.Command)
		}
		for i := range tc.cmd {
			if validation.Command[i] != tc.cmd[i] {
				t.Fatalf("command = %+v, want %+v", validation.Command, tc.cmd)
			}
		}
	}
	// Hysteria2 and Mieru have no standalone checker, so no validation command runs.
	for _, path := range []string{
		"/etc/veil/generated/hysteria2/server.yaml",
		"/etc/veil/generated/mieru/server_config.json",
	} {
		if _, ok := catalog.Match(path); ok {
			t.Fatalf("Match(%q) should be false (no standalone checker)", path)
		}
	}
	if validation, ok := catalog.Match("/tmp/other.txt"); ok || validation.Name != "" || validation.Config != "" || len(validation.Command) != 0 {
		t.Fatalf("unknown validation = %+v %v", validation, ok)
	}
}
