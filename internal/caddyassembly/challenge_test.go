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

func TestPlanAcmeChallengeBindsHTTP01ReusesPanelCaddyListener(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"x.com": {Domain: "x.com", Email: "a@x.com"},
	}
	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{
		key: {Kind: bindregistry.BindOwnerPanelCaddy},
	}
	planned, issues := PlanAcmeChallengeBinds("http-01", domains, owners)
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if _, ok := planned[key]; ok {
		t.Fatal("expected no TCP :80 challenge bind when Panel Caddy already owns the port")
	}
}

func TestPlanAcmeChallengeBindsHTTP01ReusesNaiveListener(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"x.com": {Domain: "x.com", Email: "a@x.com"},
	}
	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{
		key: {Kind: bindregistry.BindOwnerNaive},
	}
	planned, issues := PlanAcmeChallengeBinds("http-01", domains, owners)
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if _, ok := planned[key]; ok {
		t.Fatal("expected no TCP :80 challenge bind when a naive inbound already owns the port")
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

// A hysteria2-only domain under tls-alpn-01 has no Caddy TCP listener, so the
// ALPN challenge can never be answered for it. It must be switched to http-01
// and get a challenge bind on :80.
func TestPlanAcmeChallengeBindsHysteria2OnlyDomainSwitchesToHTTP01(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"hy.example.net": {
			Domain: "hy.example.net",
			Email:  "u@example.net",
			Owners: CaddyDomainOwners{HysteriaInboundNames: []string{"hy1"}},
		},
	}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{}
	planner, issues := PlanAcmeChallengeBinds("tls-alpn-01", domains, owners)
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
	owner, ok := planner[key]
	if !ok {
		t.Fatalf("expected TCP :80 challenge bind for hysteria2-only domain, got %v", planner)
	}
	if owner.ChallengeMode != "http-01" {
		t.Fatalf("expected http-01 challenge mode for hysteria2-only domain, got %q", owner.ChallengeMode)
	}
}

// When :80 is owned by a non-Caddy service, a hysteria2-only domain produces a
// warning (not a hard error) so apply still proceeds with a self-signed cert.
func TestPlanAcmeChallengeBindsHysteria2Port80ConflictIsWarning(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"hy.example.net": {
			Domain: "hy.example.net",
			Email:  "u@example.net",
			Owners: CaddyDomainOwners{HysteriaInboundNames: []string{"hy1"}},
		},
	}
	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{
		key: {Kind: bindregistry.BindOwnerPanelDirect},
	}
	_, issues := PlanAcmeChallengeBinds("tls-alpn-01", domains, owners)
	if len(issues) != 1 {
		t.Fatalf("expected one issue, got %v", issues)
	}
	if issues[0].Code != "acme_http01_port_in_use" {
		t.Fatalf("expected acme_http01_port_in_use, got %v", issues[0])
	}
	if issues[0].Severity != "warning" {
		t.Fatalf("expected warning severity for hysteria2-only domain, got %q", issues[0].Severity)
	}
}

// A domain shared with a Panel/Naive owner keeps tls-alpn-01 and a busy :80
// remains a hard error, since those rely on a working ACME cert over TCP.
func TestPlanAcmeChallengeBindsPanelDomainPort80ConflictStaysError(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"panel.example.net": {
			Domain: "panel.example.net",
			Email:  "u@example.net",
			Owners: CaddyDomainOwners{Panel: true},
		},
	}
	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{
		key: {Kind: bindregistry.BindOwnerPanelDirect},
	}
	_, issues := PlanAcmeChallengeBinds("http-01", domains, owners)
	if len(issues) != 1 || issues[0].Severity != "error" {
		t.Fatalf("expected a single error issue for panel domain, got %v", issues)
	}
}

// A hysteria2 inbound reusing the Panel's own domain must NOT switch to
// http-01: Caddy already serves that domain over TCP (tls-alpn-01 works) and
// already holds its cert. Regression test for case "hysteria2 on panel domain".
func TestPlanAcmeChallengeBindsHysteria2OnPanelDomainKeepsTLSALPN(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"panel.example.com": {
			Domain: "panel.example.com",
			Email:  "admin@example.com",
			Owners: CaddyDomainOwners{Panel: true, HysteriaInboundNames: []string{"hy1"}},
		},
	}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{}
	planned, issues := PlanAcmeChallengeBinds("tls-alpn-01", domains, owners)
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
	owner, ok := planned[key]
	if !ok {
		t.Fatalf("expected TCP :443 challenge bind for panel domain, got %v", planned)
	}
	if owner.ChallengeMode != "tls-alpn-01" {
		t.Fatalf("expected tls-alpn-01 kept for panel domain, got %q", owner.ChallengeMode)
	}
	httpKey := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
	if _, ok := planned[httpKey]; ok {
		t.Fatal("must not add an :80 http-01 bind for a panel-owned domain")
	}
}

// A hysteria2 inbound reusing a Naive inbound's domain must NOT switch to
// http-01 either: the naive Caddy listener answers the challenge. Regression
// test for case "hysteria2 on naive domain".
func TestPlanAcmeChallengeBindsHysteria2OnNaiveDomainKeepsTLSALPN(t *testing.T) {
	domains := map[string]CaddyDomainCertSpec{
		"naive.example.com": {
			Domain: "naive.example.com",
			Email:  "admin@example.com",
			Owners: CaddyDomainOwners{NaiveInboundNames: []string{"naive1"}, HysteriaInboundNames: []string{"hy1"}},
		},
	}
	owners := map[bindregistry.BindKey]bindregistry.BindOwner{}
	planned, issues := PlanAcmeChallengeBinds("tls-alpn-01", domains, owners)
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	key := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
	owner, ok := planned[key]
	if !ok {
		t.Fatalf("expected TCP :443 challenge bind for naive domain, got %v", planned)
	}
	if owner.ChallengeMode != "tls-alpn-01" {
		t.Fatalf("expected tls-alpn-01 kept for naive domain, got %q", owner.ChallengeMode)
	}
}
