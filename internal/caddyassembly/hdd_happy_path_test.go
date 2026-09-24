package caddyassembly_test

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func naiveInbound(name, domain string, publicPort int, transport string, profiles ...model.ClientProfile) model.Inbound {
	return model.Inbound{
		Name:     name,
		Protocol: "naiveproxy",
		Enabled:  true,
		Profiles: profiles,
		ProtocolFields: map[string]any{
			"domain":     domain,
			"transport":  transport,
			"publicPort": publicPort,
		},
	}
}

// TestBuildFinalRenderPlan_NaiveTCP443HappyPath verifies the happy path for a
// single naiveproxy inbound on the default public port.
func TestBuildFinalRenderPlan_NaiveTCP443HappyPath(t *testing.T) {
	settings := model.Settings{
		PanelAccess:              "direct",
		DefaultInboundPublicPort: 443,
		DefaultAcmeEmail:         "admin@vpn.example.com",
		AcmeChallengeMode:        "tls-alpn-01",
	}
	inbounds := []model.Inbound{
		naiveInbound("test", "vpn.example.com", 443, "tcp",
			model.ClientProfile{Name: "default", Username: "user", Password: "pass", Enabled: true}),
	}

	plan, owners, issues, err := caddyassembly.BuildFinalRenderPlan(settings, inbounds)
	if err != nil {
		t.Fatalf("BuildFinalRenderPlan error: %v", err)
	}
	if len(issues) > 0 {
		t.Fatalf("unexpected validation issues: %+v", issues)
	}

	naiveKey := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
	owner := owners[naiveKey]
	if owner.Kind != bindregistry.BindOwnerNaive {
		t.Fatalf("expected naive owner on TCP :443, got %+v", owner)
	}
	if owner.InboundName != "test" {
		t.Errorf("owner.InboundName = %q, want test", owner.InboundName)
	}
	// The consolidated Caddy unit pin is load-bearing for firewall/systemd
	// consumers — a blanked or renamed ServiceName must fail here (#971).
	if owner.ServiceName != "veil-caddy.service" {
		t.Errorf("owner.ServiceName = %q, want veil-caddy.service", owner.ServiceName)
	}

	server := plan.Servers[naiveKey]
	if server.Kind != caddyassembly.CaddyOwnerNaive {
		t.Fatalf("expected naive server, got %+v", server)
	}
	// The configured challenge mode must reach the plan, and tls-alpn-01 must
	// reuse the Caddy-owned :443 listener instead of adding a challenge-only
	// bind that would replace it (#970).
	if plan.DefaultChallengeMode != "tls-alpn-01" {
		t.Errorf("plan.DefaultChallengeMode = %q, want tls-alpn-01", plan.DefaultChallengeMode)
	}
	if len(plan.ACMEChallenges) != 0 {
		t.Errorf("expected no standalone ACME challenge binds for a :443-served domain, got %+v", plan.ACMEChallenges)
	}
	if server.Domain != "vpn.example.com" {
		t.Errorf("server.Domain = %q, want vpn.example.com", server.Domain)
	}
	if server.Transport != "tcp" {
		t.Errorf("server.Transport = %q, want tcp", server.Transport)
	}
	if len(server.NaiveUsers) != 1 {
		t.Fatalf("expected 1 naive user, got %d", len(server.NaiveUsers))
	}
	if server.NaiveUsers[0].Username != "user" || server.NaiveUsers[0].Password != "pass" {
		t.Errorf("unexpected credentials: %+v", server.NaiveUsers[0])
	}

	spec, ok := plan.Domains["vpn.example.com"]
	if !ok {
		t.Fatal("expected domain cert spec for vpn.example.com")
	}
	if spec.Email != "admin@vpn.example.com" {
		t.Errorf("spec.Email = %q, want admin@vpn.example.com", spec.Email)
	}
	if len(spec.Owners.NaiveInboundNames) != 1 || spec.Owners.NaiveInboundNames[0] != "test" {
		t.Errorf("unexpected domain owners: %+v", spec.Owners)
	}
}

// TestBuildFinalRenderPlan_NaiveTCP443MergesWithPanelCaddy verifies that a
// naive inbound on TCP :443 shares the Panel caddy listener: the naive inbound
// owns the bind and Panel routes are merged into the same server.
func TestBuildFinalRenderPlan_NaiveTCP443MergesWithPanelCaddy(t *testing.T) {
	settings := model.Settings{
		PanelAccess:       "caddy",
		PanelDomain:       "panel.vpn.example.com",
		PanelPublicPort:   443,
		PanelEmail:        "admin@vpn.example.com",
		DefaultAcmeEmail:  "admin@vpn.example.com",
		AcmeChallengeMode: "tls-alpn-01",
	}
	inbounds := []model.Inbound{
		naiveInbound("test", "vpn.example.com", 443, "tcp",
			model.ClientProfile{Name: "default", Username: "user", Password: "pass", Enabled: true}),
	}

	plan, owners, issues, err := caddyassembly.BuildFinalRenderPlan(settings, inbounds)
	if err != nil {
		t.Fatalf("BuildFinalRenderPlan error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("merged plan raised issues: %+v", issues)
	}

	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
	owner := owners[key]
	if owner.Kind != bindregistry.BindOwnerNaive || owner.InboundName != "test" {
		t.Fatalf("expected naive owner on TCP :443, got %+v", owner)
	}
	if owner.ServiceName != "veil-caddy.service" {
		t.Errorf("merged owner ServiceName = %q, want veil-caddy.service (#971)", owner.ServiceName)
	}
	server := plan.Servers[key]
	if server.Kind != caddyassembly.CaddyOwnerNaive {
		t.Fatalf("expected naive server, got %+v", server)
	}
	if server.PanelDomain != "panel.vpn.example.com" {
		t.Errorf("server.PanelDomain = %q, want panel.vpn.example.com", server.PanelDomain)
	}

	// Bind-owner locks alone cannot prove ACME enrollment: both the panel
	// domain and the naive domain must land in Domains with their email and
	// owner sets (#845).
	panelSpec, ok := plan.Domains["panel.vpn.example.com"]
	if !ok {
		t.Fatalf("panel domain missing from cert specs: %+v", plan.Domains)
	}
	if panelSpec.Email != "admin@vpn.example.com" || !panelSpec.Owners.Panel {
		t.Errorf("panel domain spec wrong: %+v", panelSpec)
	}
	naiveSpec, ok := plan.Domains["vpn.example.com"]
	if !ok {
		t.Fatalf("naive domain missing from cert specs: %+v", plan.Domains)
	}
	if naiveSpec.Email != "admin@vpn.example.com" {
		t.Errorf("naive domain email = %q, want admin@vpn.example.com", naiveSpec.Email)
	}
	if len(naiveSpec.Owners.NaiveInboundNames) != 1 || naiveSpec.Owners.NaiveInboundNames[0] != "test" {
		t.Errorf("naive domain owners wrong: %+v", naiveSpec.Owners)
	}
	if plan.DefaultChallengeMode != "tls-alpn-01" {
		t.Errorf("plan.DefaultChallengeMode = %q, want tls-alpn-01", plan.DefaultChallengeMode)
	}
	if len(plan.ACMEChallenges) != 0 {
		t.Errorf("shared :443 listener must not gain standalone challenge binds, got %+v", plan.ACMEChallenges)

	}
}

// TestBuildFinalRenderPlan_NaiveFlatCredentialFallback verifies that
// inbound.NaiveUsername / inbound.NaivePassword are used as credentials when no
// profiles are present, matching the validation logic in apply_plan_builder.go
// and the naiveproxy plugin fallback chain.
func TestBuildFinalRenderPlan_NaiveFlatCredentialFallback(t *testing.T) {
	settings := model.Settings{
		PanelAccess:              "direct",
		DefaultInboundPublicPort: 443,
		DefaultAcmeEmail:         "admin@vpn.example.com",
		AcmeChallengeMode:        "tls-alpn-01",
	}
	inbounds := []model.Inbound{{
		Name:          "test",
		Protocol:      "naiveproxy",
		Enabled:       true,
		NaiveUsername: "flat-user",
		NaivePassword: "flat-pass",
		ProtocolFields: map[string]any{
			"domain":     "vpn.example.com",
			"transport":  "tcp",
			"publicPort": 443,
		},
	}}

	plan, _, _, err := caddyassembly.BuildFinalRenderPlan(settings, inbounds)
	if err != nil {
		t.Fatalf("BuildFinalRenderPlan error: %v", err)
	}
	server := plan.Servers[bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}]
	if len(server.NaiveUsers) != 1 {
		t.Fatalf("expected 1 naive user, got %d", len(server.NaiveUsers))
	}
	if server.NaiveUsers[0].Username != "flat-user" || server.NaiveUsers[0].Password != "flat-pass" {
		t.Errorf("unexpected credentials: %+v", server.NaiveUsers[0])
	}
}

// TestBuildFinalRenderPlan_SameDomainDifferentPortsSharedCert verifies that two
// naive inbounds using the same domain on different public ports share domain
// certificate ownership while owning distinct bind keys.
func TestBuildFinalRenderPlan_SameDomainDifferentPortsSharedCert(t *testing.T) {
	settings := model.Settings{
		PanelAccess:              "direct",
		DefaultInboundPublicPort: 443,
		DefaultAcmeEmail:         "admin@vpn.example.com",
		AcmeChallengeMode:        "tls-alpn-01",
	}
	inbounds := []model.Inbound{
		naiveInbound("test-a", "vpn.example.com", 443, "tcp",
			model.ClientProfile{Name: "default", Username: "user-a", Password: "pass-a", Enabled: true}),
		naiveInbound("test-b", "vpn.example.com", 8443, "tcp",
			model.ClientProfile{Name: "default", Username: "user-b", Password: "pass-b", Enabled: true}),
	}

	plan, owners, _, err := caddyassembly.BuildFinalRenderPlan(settings, inbounds)
	if err != nil {
		t.Fatalf("BuildFinalRenderPlan error: %v", err)
	}

	conflicts := bindregistry.ValidateNoConflicts(owners)
	if len(conflicts) != 0 {
		t.Fatalf("unexpected bind conflicts: %+v", conflicts)
	}

	tcp443 := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
	tcp8443 := bindregistry.BindKey{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}
	if owners[tcp443].InboundName != "test-a" {
		t.Errorf("TCP :443 owner = %+v, want test-a", owners[tcp443])
	}
	if owners[tcp8443].InboundName != "test-b" {
		t.Errorf("TCP :8443 owner = %+v, want test-b", owners[tcp8443])
	}

	spec, ok := plan.Domains["vpn.example.com"]
	if !ok {
		t.Fatal("expected shared domain cert spec for vpn.example.com")
	}
	if len(spec.Owners.NaiveInboundNames) != 2 {
		t.Fatalf("expected 2 naive owners, got %d: %+v", len(spec.Owners.NaiveInboundNames), spec.Owners.NaiveInboundNames)
	}
	if spec.Email != "admin@vpn.example.com" {
		t.Errorf("spec.Email = %q, want admin@vpn.example.com", spec.Email)
	}

	if len(plan.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(plan.Servers))
	}
}
