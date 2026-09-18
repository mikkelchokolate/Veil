package renderer

import (
	"encoding/json"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

// Regression coverage for the controlled-CA plumbing (audit #304):
// VEIL_ACME_CA_URL/VEIL_ACME_CA_ROOT must reach the rendered Caddy ACME
// issuer so the install-acceptance caddy leg can issue against pebble.
func TestRenderCaddyJSONControlledCAIssuer(t *testing.T) {
	t.Setenv("VEIL_ACME_CA_URL", " https://127.0.0.1:14000/dir ")
	t.Setenv("VEIL_ACME_CA_ROOT", " /etc/veil/acme-root.pem ")
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"panel.example.com": {
				Domain: "panel.example.com",
				Email:  "a@example.com",
				Owners: caddyassembly.CaddyDomainOwners{Panel: true},
			},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	issuer := decodePanelIssuer(t, data)
	if issuer["ca"] != "https://127.0.0.1:14000/dir" {
		t.Fatalf("issuer.ca = %v, want the controlled CA URL", issuer["ca"])
	}
	roots, ok := issuer["trusted_roots_pem_files"].([]any)
	if !ok || len(roots) != 1 || roots[0] != "/etc/veil/acme-root.pem" {
		t.Fatalf("issuer.trusted_roots_pem_files = %v, want [/etc/veil/acme-root.pem]", issuer["trusted_roots_pem_files"])
	}
}

func TestRenderCaddyJSONDefaultsToLetsEncryptIssuer(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"panel.example.com": {
				Domain: "panel.example.com",
				Email:  "a@example.com",
				Owners: caddyassembly.CaddyDomainOwners{Panel: true},
			},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	issuer := decodePanelIssuer(t, data)
	if _, ok := issuer["ca"]; ok {
		t.Fatalf("issuer.ca = %v, want it omitted for the default Let's Encrypt issuer", issuer["ca"])
	}
	if _, ok := issuer["trusted_roots_pem_files"]; ok {
		t.Fatalf("issuer.trusted_roots_pem_files = %v, want it omitted by default", issuer["trusted_roots_pem_files"])
	}
}

func decodePanelIssuer(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var cfg struct {
		Apps struct {
			TLS struct {
				Automation struct {
					Policies []struct {
						Issuers []map[string]any `json:"issuers"`
					} `json:"policies"`
				} `json:"automation"`
			} `json:"tls"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode caddy json: %v", err)
	}
	if len(cfg.Apps.TLS.Automation.Policies) != 1 || len(cfg.Apps.TLS.Automation.Policies[0].Issuers) != 1 {
		t.Fatalf("expected exactly one automation policy with one issuer: %+v", cfg.Apps.TLS.Automation.Policies)
	}
	return cfg.Apps.TLS.Automation.Policies[0].Issuers[0]
}
