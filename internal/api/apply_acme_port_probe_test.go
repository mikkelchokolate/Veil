package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

func stubHostPortAvailable(t *testing.T, available bool, err error) {
	t.Helper()
	prev := hostPortAvailable
	hostPortAvailable = func(context.Context, string, int) (bool, error) {
		return available, err
	}
	t.Cleanup(func() { hostPortAvailable = prev })
}

func hy2OnlyDomainPlan(t *testing.T) (caddyassembly.CaddyRenderPlan, map[bindregistry.BindKey]bindregistry.BindOwner) {
	t.Helper()
	settings := Settings{
		PanelListen:        "127.0.0.1:2096",
		Mode:               "server",
		AcmeChallengeMode:  "tls-alpn-01",
		DefaultAcmeEmail:   "admin@example.com",
		PanelAccess:        "direct",
	}
	inbounds := []Inbound{{
		Name:           "hy2",
		Protocol:       "hysteria2",
		Transport:      "udp",
		Port:           443,
		Enabled:        true,
		Password:       "secret",
		ProtocolFields: map[string]any{"domain": "hy2-only.example.com"},
	}}
	plan, owners, _, err := caddyassembly.BuildFinalRenderPlan(settings, inbounds)
	if err != nil {
		t.Fatalf("BuildFinalRenderPlan: %v", err)
	}
	return plan, owners
}

func tlsAutomationIssuerChallenges(t *testing.T, plan caddyassembly.CaddyRenderPlan) map[string]any {
	t.Helper()
	data, err := renderer.RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
	if err != nil {
		t.Fatalf("RenderCaddyJSON: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("caddy json: %v", err)
	}
	policies := cfg["apps"].(map[string]any)["tls"].(map[string]any)["automation"].(map[string]any)["policies"].([]any)
	if len(policies) != 1 {
		t.Fatalf("policies = %d, want 1", len(policies))
	}
	return policies[0].(map[string]any)["issuers"].([]any)[0].(map[string]any)["challenges"].(map[string]any)
}

// A foreign service on :80 demotes the hy2-only http-01 challenge bind to a
// warning, but the domain must KEEP http-01 as its issuer mode (#1168 review):
// without the deferral the issuer silently falls back to tls-alpn-01, which a
// UDP-only domain can never solve — the post-apply retry window would never
// converge even after the port frees.
func TestDemoteForeignHeldHTTP01KeepsIssuerHTTP01(t *testing.T) {
	stubHostPortAvailable(t, false, nil)
	plan, owners := hy2OnlyDomainPlan(t)
	httpKey := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
	if _, ok := plan.ACMEChallenges[httpKey]; !ok {
		t.Fatalf("plan has no :80 http-01 challenge bind: %+v", plan.ACMEChallenges)
	}

	issues := demoteForeignHeldHTTP01Binds(context.Background(), &plan, owners, t.TempDir())
	if len(issues) != 1 || issues[0].Severity != "warning" || issues[0].Code != "acme_http01_port_in_use" {
		t.Fatalf("demote issues = %+v, want one acme_http01_port_in_use warning", issues)
	}
	if _, ok := plan.ACMEChallenges[httpKey]; ok {
		t.Fatal("demoted bind still in ACMEChallenges")
	}
	if _, ok := owners[httpKey]; ok {
		t.Fatal("demoted bind still in owner registry")
	}
	if !plan.HTTP01DeferredDomains["hy2-only.example.com"] {
		t.Fatalf("domain not recorded as deferred http-01: %+v", plan.HTTP01DeferredDomains)
	}

	challenges := tlsAutomationIssuerChallenges(t, plan)
	if disabled, _ := challenges["http"].(map[string]any)["disabled"].(bool); disabled {
		t.Fatalf("demoted domain issuer lost http-01: %+v", challenges)
	}
	if disabled, _ := challenges["tls-alpn"].(map[string]any)["disabled"].(bool); !disabled {
		t.Fatalf("issuer unexpectedly kept tls-alpn enabled: %+v", challenges)
	}

	data, err := renderer.RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
	if err != nil {
		t.Fatalf("RenderCaddyJSON: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	servers := cfg["apps"].(map[string]any)["http"].(map[string]any)["servers"].(map[string]any)
	for name := range servers {
		if strings.HasSuffix(name, "-acme") {
			t.Fatalf("demoted challenge server still rendered: %s", name)
		}
	}
}

// A free :80 keeps the bind as planned — no demote, no deferral.
func TestForeignHeldHTTP01FreePortKeepsBind(t *testing.T) {
	stubHostPortAvailable(t, true, nil)
	plan, owners := hy2OnlyDomainPlan(t)
	httpKey := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}

	issues := demoteForeignHeldHTTP01Binds(context.Background(), &plan, owners, t.TempDir())
	if len(issues) != 0 {
		t.Fatalf("free :80 produced issues: %+v", issues)
	}
	if _, ok := plan.ACMEChallenges[httpKey]; !ok {
		t.Fatal("free :80 lost the planned challenge bind")
	}
	if len(plan.HTTP01DeferredDomains) != 0 {
		t.Fatalf("unexpected deferred domains: %+v", plan.HTTP01DeferredDomains)
	}
}

// A busy :80 shared with a Panel/Naive domain is still a hard error — those
// listeners genuinely need the port and the apply must not proceed.
func TestForeignHeldHTTP01SharedDomainStaysError(t *testing.T) {
	stubHostPortAvailable(t, false, nil)
	settings := Settings{
		PanelListen:       "127.0.0.1:2096",
		Mode:              "server",
		AcmeChallengeMode: "http-01",
		DefaultAcmeEmail:  "admin@example.com",
		PanelAccess:       "caddy",
		PanelDomain:       "panel.example.com",
		PanelPublicPort:   443,
	}
	inbounds := []Inbound{{
		Name:           "hy2",
		Protocol:       "hysteria2",
		Transport:      "udp",
		Port:           8443,
		Enabled:        true,
		Password:       "secret",
		ProtocolFields: map[string]any{"domain": "panel.example.com"},
	}}
	plan, owners, _, err := caddyassembly.BuildFinalRenderPlan(settings, inbounds)
	if err != nil {
		t.Fatalf("BuildFinalRenderPlan: %v", err)
	}
	httpKey := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
	if _, ok := plan.ACMEChallenges[httpKey]; !ok {
		t.Fatalf("plan has no :80 http-01 challenge bind: %+v", plan.ACMEChallenges)
	}

	issues := demoteForeignHeldHTTP01Binds(context.Background(), &plan, owners, t.TempDir())
	if len(issues) != 1 || issues[0].Severity != "error" {
		t.Fatalf("shared :80 domain issues = %+v, want one error", issues)
	}
	if _, ok := plan.ACMEChallenges[httpKey]; !ok {
		t.Fatal("shared-domain bind was demoted")
	}
}
