package api

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedConfigSetAllowsMultipleEnabledInboundsPerProtocol(t *testing.T) {
	_, err := BuildGeneratedConfigSet(GeneratedConfigInput{
		ApplyRoot: t.TempDir(),
		Settings: Settings{
			Domain:            "vpn.example.com",
			DefaultAcmeEmail:  "admin@example.com",
			NaiveUsername:     "veil",
			NaivePassword:     "global-naive",
			Hysteria2Password: "global-hy2",
			MasqueradeURL:     "https://www.bing.com/",
			FallbackRoot:      "/var/lib/veil/www",
		},
		Inbounds: []Inbound{
			{Name: "naive-a", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "a"},
			{Name: "naive-b", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true, Password: "b"},
		},
	})
	if err != nil {
		t.Fatalf("expected no error for multiple enabled naiveproxy inbounds, got %v", err)
	}
}

func TestGeneratedConfigSetUsesClientProfiles(t *testing.T) {
	applyRoot := t.TempDir()
	configs, err := BuildGeneratedConfigSet(GeneratedConfigInput{
		ApplyRoot: applyRoot,
		Settings: Settings{
			Domain:            "vpn.example.com",
			DefaultAcmeEmail:  "admin@example.com",
			NaiveUsername:     "veil",
			NaivePassword:     "global-naive",
			Hysteria2Password: "global-hy2",
			MasqueradeURL:     "https://www.bing.com/",
			FallbackRoot:      "/var/lib/veil/www",
		},
		Inbounds: []Inbound{
			{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Profiles: []ClientProfile{
				{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: true},
				{Name: "bob", Username: "bob", Password: "bob-pass", Enabled: true},
			}},
			{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true, Profiles: []ClientProfile{
				{Name: "carol", Username: "carol", Password: "carol-pass", Enabled: true},
				{Name: "dave", Username: "dave", Password: "dave-pass", Enabled: true},
			}},
		},
	})
	if err != nil {
		t.Fatalf("BuildGeneratedConfigSet: %v", err)
	}
	caddy := configs[filepath.Join(applyRoot, "generated", "caddy", "config.json")]
	for _, want := range []string{
		"forward_proxy",
		forwardProxyJSONCredential("alice", "alice-pass"),
		forwardProxyJSONCredential("bob", "bob-pass"),
	} {
		if !strings.Contains(caddy, want) {
			t.Fatalf("Caddy JSON missing %q:\n%s", want, caddy)
		}
	}
	hy2 := configs[filepath.Join(applyRoot, "generated", "hysteria2", "hy2.yaml")]
	for _, want := range []string{"type: userpass", "carol: carol-pass", "dave: dave-pass"} {
		if !strings.Contains(hy2, want) {
			t.Fatalf("Hysteria2 config missing %q:\n%s", want, hy2)
		}
	}
}

func TestGeneratedConfigSetUsesPerInboundPasswords(t *testing.T) {
	applyRoot := t.TempDir()
	configs, err := BuildGeneratedConfigSet(GeneratedConfigInput{
		ApplyRoot: applyRoot,
		Settings: Settings{
			Domain:            "vpn.example.com",
			DefaultAcmeEmail:  "admin@example.com",
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
	caddy := configs[filepath.Join(applyRoot, "generated", "caddy", "config.json")]
	if !strings.Contains(caddy, forwardProxyJSONCredential("veil", "vip-naive")) {
		t.Fatalf("Caddy JSON should use per-inbound naive password:\n%s", caddy)
	}
	hy2 := configs[filepath.Join(applyRoot, "generated", "hysteria2", "hy2-vip.yaml")]
	if !strings.Contains(hy2, "password: vip-hy2") {
		t.Fatalf("Hysteria2 config should use per-inbound password:\n%s", hy2)
	}
}

func forwardProxyJSONCredential(username, password string) string {
	basicValue := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return base64.StdEncoding.EncodeToString([]byte(basicValue))
}
