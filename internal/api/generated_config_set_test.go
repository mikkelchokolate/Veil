package api

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedConfigSetUsesPerInboundPasswords(t *testing.T) {
	applyRoot := t.TempDir()
	configs, err := BuildGeneratedConfigSet(GeneratedConfigInput{
		ApplyRoot: applyRoot,
		Settings: Settings{
			Domain:            "vpn.example.com",
			Email:             "admin@example.com",
			Stack:             "both",
			NaiveUsername:     "veil",
			NaivePassword:     "global-naive",
			Hysteria2Password: "global-hy2",
			MasqueradeURL:     "https://www.bing.com/",
			FallbackRoot:      "/var/lib/veil/www",
		},
		Inbounds: []Inbound{
			{Name: "naive-vip", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true, Password: "vip-naive"},
			{Name: "hy2-vip", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true, Password: "vip-hy2"},
		},
	})
	if err != nil {
		t.Fatalf("BuildGeneratedConfigSet: %v", err)
	}
	caddy := configs[filepath.Join(applyRoot, "generated", "caddy", "Caddyfile")]
	if !strings.Contains(caddy, "basic_auth veil vip-naive") {
		t.Fatalf("Caddyfile should use per-inbound naive password:\n%s", caddy)
	}
	hy2 := configs[filepath.Join(applyRoot, "generated", "hysteria2", "server.yaml")]
	if !strings.Contains(hy2, "password: vip-hy2") {
		t.Fatalf("Hysteria2 config should use per-inbound password:\n%s", hy2)
	}
}
