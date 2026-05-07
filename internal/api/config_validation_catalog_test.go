package api

import "testing"

func TestConfigValidationCatalogMatchesKnownGeneratedConfigs(t *testing.T) {
	catalog := NewConfigValidationCatalog()
	cases := []struct {
		path string
		name string
		cmd  []string
	}{
		{"/etc/veil/generated/caddy/Caddyfile", "caddy", []string{"caddy", "validate", "--config", "/etc/veil/generated/caddy/Caddyfile"}},
		{"/etc/veil/generated/hysteria2/server.yaml", "hysteria2", []string{"hysteria", "server", "--config", "/etc/veil/generated/hysteria2/server.yaml", "--check"}},
		{"/etc/veil/generated/sing-box/warp.json", "warp", []string{"sing-box", "check", "-c", "/etc/veil/generated/sing-box/warp.json"}},
		{"/etc/veil/generated/mieru/server_config.json", "mieru", []string{"mieru", "check", "-c", "/etc/veil/generated/mieru/server_config.json"}},
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
	if validation, ok := catalog.Match("/tmp/other.txt"); ok || validation.Name != "" || validation.Config != "" || len(validation.Command) != 0 {
		t.Fatalf("unknown validation = %+v %v", validation, ok)
	}
}
