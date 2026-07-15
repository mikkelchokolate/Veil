package caddyassembly

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestResolveDomainCertSpecsConflictingEmail(t *testing.T) {
	settings := model.Settings{PanelAccess: "direct"}
	inbounds := []model.Inbound{
		{Name: "n1", Protocol: "naiveproxy", ProtocolFields: map[string]any{"domain": "x.com", "email": "a@x.com"}},
		{Name: "n2", Protocol: "naiveproxy", ProtocolFields: map[string]any{"domain": "x.com", "email": "b@x.com"}},
	}
	_, err := ResolveDomainCertSpecs(settings, inbounds)
	if err == nil {
		t.Fatal("expected conflicting email error")
	}
}

func TestResolveDomainCertSpecsFallback(t *testing.T) {
	settings := model.Settings{PanelAccess: "direct", DefaultAcmeEmail: "admin@x.com"}
	inbounds := []model.Inbound{
		{Name: "n1", Protocol: "naiveproxy", ProtocolFields: map[string]any{"domain": "x.com"}},
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
		{Name: "hy2", Protocol: "hysteria2", ProtocolFields: map[string]any{"domain": "hy.example.com"}},
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
		{Name: "n1", Protocol: "naiveproxy", ProtocolFields: map[string]any{"domain": "x.com"}},
	}
	_, err := ResolveDomainCertSpecs(settings, inbounds)
	if err == nil {
		t.Fatal("expected error when no explicit/default/panel email is available")
	}
}
