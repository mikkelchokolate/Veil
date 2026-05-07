package api

import "testing"

func TestSystemdServiceStatusParserParsesKnownProperties(t *testing.T) {
	status := NewSystemdServiceStatusParser().Parse("caddy.service", `LoadState=loaded
ActiveState=active
SubState=running
Ignored=value
`)
	if status.Unit != "caddy.service" || status.LoadState != "loaded" || status.ActiveState != "active" || status.SubState != "running" {
		t.Fatalf("status = %+v", status)
	}
}

func TestSystemdServiceStatusParserDefaultsUnknownStates(t *testing.T) {
	status := NewSystemdServiceStatusParser().Parse("caddy.service", "")
	if status.LoadState != "unknown" || status.ActiveState != "unknown" || status.SubState != "unknown" {
		t.Fatalf("status = %+v", status)
	}
}
