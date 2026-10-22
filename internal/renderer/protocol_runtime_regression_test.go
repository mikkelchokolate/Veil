package renderer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

// Chromium-based Naive clients require a publicly trusted chain, so the
// production Caddy JSON must never fall back to Caddy's internal CA for a
// naive domain: every automation issuer is ACME and the naive subject is
// covered by an ACME policy carrying the operator's email. This replaces the
// Caddyfile-path guard removed with the dead text-template renderer (#1292).
func TestRenderCaddyJSONNaiveUsesPublicTLSOnly(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}: {
				Kind:         caddyassembly.CaddyOwnerNaive,
				Domain:       "vpn.example.com",
				InboundName:  "naive",
				Transport:    "tcp",
				NaiveUsers:   []caddyassembly.CaddyNaiveUser{{Username: "alice", Password: "secret"}},
				FallbackRoot: "/etc/veil/www",
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"vpn.example.com": {Domain: "vpn.example.com", Email: "admin@example.com"},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
	if err != nil {
		t.Fatalf("RenderCaddyJSON: %v", err)
	}
	var cfg struct {
		Apps struct {
			TLS struct {
				Automation struct {
					Policies []struct {
						Subjects []string         `json:"subjects"`
						Issuers  []map[string]any `json:"issuers"`
					} `json:"policies"`
				} `json:"automation"`
			} `json:"tls"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}
	covered := false
	for _, policy := range cfg.Apps.TLS.Automation.Policies {
		if len(policy.Issuers) == 0 {
			t.Fatalf("automation policy %v has no explicit issuer, so Caddy would pick its defaults (including internal):\n%s", policy.Subjects, data)
		}
		for _, issuer := range policy.Issuers {
			if issuer["module"] != "acme" {
				t.Fatalf("automation policy %v uses issuer %v; naive TLS must be publicly trusted:\n%s", policy.Subjects, issuer["module"], data)
			}
		}
		for _, subject := range policy.Subjects {
			if subject == "vpn.example.com" && policy.Issuers[0]["email"] == "admin@example.com" {
				covered = true
			}
		}
	}
	if !covered {
		t.Fatalf("naive domain is not covered by an ACME policy with the operator email:\n%s", data)
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
