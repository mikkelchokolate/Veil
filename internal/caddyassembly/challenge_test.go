package caddyassembly

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
)

func TestPlanAcmeChallengeBindsHTTP01(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"x.com": {Domain: "x.com", Email: "a@x.com"},
	}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{}
	planned, issues := PlanAcmeChallengeBinds("http-01", domains, owners)
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
	if _, ok := planned[key]; !ok {
		t.Fatal("expected TCP :80 challenge bind")
	}
}

func TestPlanAcmeChallengeBindsDNS01NoBind(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"x.com": {Domain: "x.com", Email: "a@x.com"},
	}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{}
	planned, issues := PlanAcmeChallengeBinds("dns-01", domains, owners)
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if len(planned) != 0 {
		t.Fatalf("dns-01 should add no binds, got %v", planned)
	}
}

func TestPlanAcmeChallengeBindsTLSALPN01(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"x.com": {Domain: "x.com", Email: "a@x.com"},
	}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{}
	planned, issues := PlanAcmeChallengeBinds("tls-alpn-01", domains, owners)
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
	if _, ok := planned[key]; !ok {
		t.Fatal("expected TCP :443 challenge bind")
	}
}

func TestPlanAcmeChallengeBindsHTTP01Conflict(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"x.com": {Domain: "x.com", Email: "a@x.com"},
	}
	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{
		key: {Kind: bindregistry.BindOwnerPanelDirect},
	}
	planned, issues := PlanAcmeChallengeBinds("http-01", domains, owners)
	if len(planned) != 0 {
		t.Fatalf("expected no planned binds on conflict, got %v", planned)
	}
	if len(issues) != 1 {
		t.Fatalf("expected one issue, got %v", issues)
	}
	if issues[0].Code != "acme_http01_port_in_use" {
		t.Fatalf("expected acme_http01_port_in_use issue, got %v", issues[0])
	}
}

func TestPlanAcmeChallengeBindsTLSALPN01Conflict(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"x.com": {Domain: "x.com", Email: "a@x.com"},
	}
	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{
		key: {Kind: bindregistry.BindOwnerPanelDirect},
	}
	planned, issues := PlanAcmeChallengeBinds("tls-alpn-01", domains, owners)
	if len(planned) != 0 {
		t.Fatalf("expected no planned binds on conflict, got %v", planned)
	}
	if len(issues) != 1 {
		t.Fatalf("expected one issue, got %v", issues)
	}
	if issues[0].Code != "acme_tlsalpn_port_in_use" {
		t.Fatalf("expected acme_tlsalpn_port_in_use issue, got %v", issues[0])
	}
}

func TestPlanAcmeChallengeBindsTLSALPN01ReusesCaddyListener(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"x.com": {Domain: "x.com", Email: "a@x.com"},
	}
	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{
		key: {Kind: bindregistry.BindOwnerPanelCaddy},
	}
	planned, issues := PlanAcmeChallengeBinds("tls-alpn-01", domains, owners)
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if _, ok := planned[key]; ok {
		t.Fatal("expected no TCP :443 challenge bind when a compatible Caddy listener already owns the port")
	}
}
