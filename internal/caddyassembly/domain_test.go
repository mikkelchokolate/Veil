package caddyassembly

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestResolveDomainCertSpecsConflictingEmail(t *testing.T) {
	settings := model.Settings{PanelAccess: "direct"}
	inbounds := []model.Inbound{
		{Name: "n1", Protocol: "naiveproxy", Enabled: true, ProtocolFields: map[string]any{"domain": "x.com", "email": "a@x.com"}},
		{Name: "n2", Protocol: "naiveproxy", Enabled: true, ProtocolFields: map[string]any{"domain": "x.com", "email": "b@x.com"}},
	}
	_, err := ResolveDomainCertSpecs(settings, inbounds)
	if err == nil {
		t.Fatal("expected conflicting email error")
	}
}

func TestResolveDomainCertSpecsFallback(t *testing.T) {
	settings := model.Settings{PanelAccess: "direct", DefaultAcmeEmail: "admin@x.com"}
	inbounds := []model.Inbound{
		{Name: "n1", Protocol: "naiveproxy", Enabled: true, ProtocolFields: map[string]any{"domain": "x.com"}},
	}
	specs, err := ResolveDomainCertSpecs(settings, inbounds)
	if err != nil {
		t.Fatal(err)
	}
	if specs["x.com"].Email != "admin@x.com" {
		t.Errorf("expected fallback email, got %q", specs["x.com"].Email)
	}
}

func TestResolveDomainCertSpecsIncludesHysteria2OnlyDomain(t *testing.T) {
	settings := model.Settings{PanelAccess: "direct", DefaultAcmeEmail: "admin@x.com"}
	inbounds := []model.Inbound{
		{Name: "hy2", Protocol: "hysteria2", Enabled: true, ProtocolFields: map[string]any{"domain": "hy.example.com"}},
	}
	specs, err := ResolveDomainCertSpecs(settings, inbounds)
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := specs["hy.example.com"]
	if !ok {
		t.Fatalf("expected hysteria2-only domain in specs, got %+v", specs)
	}
	if spec.Email != "admin@x.com" {
		t.Errorf("expected email admin@x.com, got %q", spec.Email)
	}
	if len(spec.Owners.HysteriaInboundNames) != 1 || spec.Owners.HysteriaInboundNames[0] != "hy2" {
		t.Errorf("expected hysteria2 owner hy2, got %+v", spec.Owners.HysteriaInboundNames)
	}
}

func TestResolveDomainCertSpecsIgnoresLegacyGlobalEmailForNaive(t *testing.T) {
	settings := model.Settings{PanelAccess: "direct", Email: "legacy@x.com"}
	inbounds := []model.Inbound{
		{Name: "n1", Protocol: "naiveproxy", Enabled: true, ProtocolFields: map[string]any{"domain": "x.com"}},
	}
	_, err := ResolveDomainCertSpecs(settings, inbounds)
	if err == nil {
		t.Fatal("expected error when no explicit/default/panel email is available")
	}
}

// TestResolveDomainCertSpecsSkipsDisabledInbounds covers audit #306: a
// disabled inbound must not enroll a certificate domain, require an email, or
// conflict with an enabled domain's email.
func TestResolveDomainCertSpecsSkipsDisabledInbounds(t *testing.T) {
	settings := model.Settings{PanelAccess: "direct"}
	specs, err := ResolveDomainCertSpecs(settings, []model.Inbound{
		{Name: "hy-off", Protocol: "hysteria2", Enabled: false, ProtocolFields: map[string]any{"domain": "unused.example.com"}},
	})
	if err != nil {
		t.Fatalf("disabled inbound caused error: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("disabled inbound enrolled domains: %+v", specs)
	}
}

// TestResolveDomainCertSpecsDisabledDoesNotConflictWithEnabled ensures a
// disabled entry's email cannot conflict with an enabled owner of the same
// domain (audit #306).
func TestResolveDomainCertSpecsDisabledDoesNotConflictWithEnabled(t *testing.T) {
	settings := model.Settings{PanelAccess: "direct", DefaultAcmeEmail: "admin@x.com"}
	specs, err := ResolveDomainCertSpecs(settings, []model.Inbound{
		{Name: "n1", Protocol: "naiveproxy", Enabled: true, ProtocolFields: map[string]any{"domain": "x.com", "email": "a@x.com"}},
		{Name: "n2", Protocol: "naiveproxy", Enabled: false, ProtocolFields: map[string]any{"domain": "x.com", "email": "b@x.com"}},
		{Name: "hy-off", Protocol: "hysteria2", Enabled: false, ProtocolFields: map[string]any{"domain": "hy.example.com"}},
	})
	if err != nil {
		t.Fatalf("disabled inbounds must not conflict: %v", err)
	}
	if len(specs) != 1 || specs["x.com"].Email != "a@x.com" {
		t.Fatalf("specs = %+v", specs)
	}
	if len(specs["x.com"].Owners.NaiveInboundNames) != 1 || specs["x.com"].Owners.NaiveInboundNames[0] != "n1" {
		t.Fatalf("disabled inbound listed as owner: %+v", specs["x.com"].Owners)
	}
}
