package renderer

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderNaiveCaddyfileUsesPublicTLSOnly(t *testing.T) {
	body, err := RenderNaiveCaddyfile(NaiveConfig{
		Domain:     "vpn.example.com",
		Email:      "admin@example.com",
		ListenPort: 443,
		Username:   "alice",
		Password:   "secret",
	})
	if err != nil {
		t.Fatalf("RenderNaiveCaddyfile: %v", err)
	}
	for _, want := range []string{
		":443, vpn.example.com {",
		"tls admin@example.com",
		"encode",
		"forward_proxy",
		"probe_resistance",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Caddyfile missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "issuer internal") {
		t.Fatalf("production NaiveProxy Caddyfile must not silently issue an untrusted internal certificate:\n%s", body)
	}
}

func TestRenderMieruClientUsesIPAddressForLiteralIP(t *testing.T) {
	body, err := RenderMieruClient(MieruClientConfig{
		ProfileName:  "default",
		DomainName:   "127.0.0.1",
		PortBindings: []MieruPortBinding{{Port: 8443, Protocol: "udp"}},
		User:         MieruUser{Name: "alice", Password: "secret"},
		Socks5Port:   1080,
		RPCPort:      8964,
	})
	if err != nil {
		t.Fatalf("RenderMieruClient: %v", err)
	}
	var decoded struct {
		Profiles []struct {
			Servers []struct {
				IPAddress  string `json:"ipAddress"`
				DomainName string `json:"domainName"`
			} `json:"servers"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	server := decoded.Profiles[0].Servers[0]
	if server.IPAddress != "127.0.0.1" || server.DomainName != "" {
		t.Fatalf("server endpoint = ip %q domain %q", server.IPAddress, server.DomainName)
	}
}

func TestRenderMieruClientUsesDomainNameForHostname(t *testing.T) {
	body, err := RenderMieruClient(MieruClientConfig{
		ProfileName:  "default",
		DomainName:   "vpn.example.com",
		PortBindings: []MieruPortBinding{{Port: 443, Protocol: "tcp"}},
		User:         MieruUser{Name: "alice", Password: "secret"},
		Socks5Port:   1080,
		RPCPort:      8964,
	})
	if err != nil {
		t.Fatalf("RenderMieruClient: %v", err)
	}
	if !strings.Contains(body, `"domainName": "vpn.example.com"`) {
		t.Fatalf("client config missing domain endpoint:\n%s", body)
	}
	if strings.Contains(body, `"ipAddress"`) {
		t.Fatalf("client config unexpectedly contains ipAddress:\n%s", body)
	}
}

func TestRenderMieruRejectsConflictingDuplicateUser(t *testing.T) {
	_, err := RenderMieru(MieruConfig{
		PortBindings: []MieruPortBinding{{Port: 443, Protocol: "tcp"}},
		Users: []MieruUser{
			{Name: "alice", Password: "one"},
			{Name: "alice", Password: "two"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting passwords") {
		t.Fatalf("error = %v, want conflicting password error", err)
	}
}
