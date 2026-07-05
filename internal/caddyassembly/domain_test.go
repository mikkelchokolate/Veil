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
