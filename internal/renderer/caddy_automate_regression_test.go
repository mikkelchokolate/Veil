package renderer

import (
	"encoding/json"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

// TestRenderCaddyJSONEnrollsHysteria2OnlyDomainViaAutomate covers audit #308:
// automation policy subjects only select a policy; a domain with no HTTP host
// matcher (Hysteria2-only) must be enrolled through certificates.automate or
// Caddy never requests its first certificate.
func TestRenderCaddyJSONEnrollsHysteria2OnlyDomainViaAutomate(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"hy.example.com": {
				Domain: "hy.example.com",
				Email:  "a@example.com",
				Owners: caddyassembly.CaddyDomainOwners{HysteriaInboundNames: []string{"hy2"}},
			},
			"naive.example.com": {
				Domain: "naive.example.com",
				Email:  "a@example.com",
				Owners: caddyassembly.CaddyDomainOwners{NaiveInboundNames: []string{"n1"}},
			},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Apps struct {
			TLS struct {
				Certificates struct {
					Automate []string `json:"automate"`
				} `json:"certificates"`
			} `json:"tls"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode caddy json: %v", err)
	}
	automate := cfg.Apps.TLS.Certificates.Automate
	if len(automate) != 1 || automate[0] != "hy.example.com" {
		t.Fatalf("automate = %v, want [hy.example.com] (naive domain is enrolled by host matchers)", automate)
	}
}

// TestRenderCaddyJSONPanelAndNaiveDomainsNotAutomated ensures host-matched
// domains — both the panel domain and a Naive inbound domain — are not
// redundantly enrolled through certificates.automate (audit #308, #847).
func TestRenderCaddyJSONPanelAndNaiveDomainsNotAutomated(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"panel.example.com": {
				Domain: "panel.example.com",
				Email:  "a@example.com",
				Owners: caddyassembly.CaddyDomainOwners{Panel: true},
			},
			"naive.example.com": {
				Domain: "naive.example.com",
				Email:  "a@example.com",
				Owners: caddyassembly.CaddyDomainOwners{NaiveInboundNames: []string{"n1"}},
			},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Apps struct {
			TLS struct {
				Certificates *struct {
					Automate []string `json:"automate"`
				} `json:"certificates"`
			} `json:"tls"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("decode caddy json: %v", err)
	}
	if cfg.Apps.TLS.Certificates != nil {
		t.Fatalf("panel-owned domain must not be automate-enrolled: %+v", cfg.Apps.TLS.Certificates)
	}
}
