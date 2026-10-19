package managementstate

import (
	"strings"
	"testing"
)

// Issue #1291 end to end through the real protocol registry schemas: the
// panel UI submits panelDomain via protocolFields, and a wildcard must never
// be persisted (it renders a {"host":["*"]} matcher on the panel and /s/*
// routes).
func TestManagementStateMutationRejectsWildcardPanelDomain(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server", PanelDomain: "panel.example.com"}
	saves := 0
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Settings: &settings}, func() error {
		saves++
		return nil
	})

	_, err := mutation.UpdateSettings(Settings{
		PanelListen:    "127.0.0.1:2096",
		Mode:           "server",
		ProtocolFields: map[string]any{"panelDomain": "*"},
	})
	if err == nil || !strings.Contains(err.Error(), "panelDomain") {
		t.Fatalf("err = %v, want panelDomain rejection", err)
	}
	if saves != 0 || settings.PanelDomain != "panel.example.com" {
		t.Fatalf("invalid update leaked: saves=%d PanelDomain=%q", saves, settings.PanelDomain)
	}

	if _, err := mutation.UpdateSettings(Settings{
		PanelListen:    "127.0.0.1:2096",
		Mode:           "server",
		ProtocolFields: map[string]any{"panelDomain": "New.Example.COM", "panelPublicPort": float64(99999)},
	}); err == nil || !strings.Contains(err.Error(), "panelPublicPort") {
		t.Fatalf("err = %v, want panelPublicPort rejection", err)
	}

	if _, err := mutation.UpdateSettings(Settings{
		PanelListen:    "127.0.0.1:2096",
		Mode:           "server",
		ProtocolFields: map[string]any{"panelDomain": "New.Example.COM"},
	}); err != nil {
		t.Fatalf("valid update: %v", err)
	}
	if saves != 1 || settings.PanelDomain != "new.example.com" {
		t.Fatalf("valid update: saves=%d PanelDomain=%q, want folded new.example.com", saves, settings.PanelDomain)
	}
}
