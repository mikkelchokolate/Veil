package service

import "testing"

func TestSystemdStatusParserDefaultsAndParsesStates(t *testing.T) {
	status := NewSystemdServiceStatusParser().Parse("veil.service", "LoadState=loaded\nActiveState=active\nSubState=running\n")
	if status.Unit != "veil.service" || status.LoadState != "loaded" || status.ActiveState != "active" || status.SubState != "running" {
		t.Fatalf("status = %+v", status)
	}
	missing := NewSystemdServiceStatusParser().Parse("veil.service", "")
	if missing.LoadState != "unknown" || missing.ActiveState != "unknown" || missing.SubState != "unknown" {
		t.Fatalf("missing defaults = %+v", missing)
	}
}
