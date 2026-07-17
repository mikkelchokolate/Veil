package settings

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

// A schema key that is also a dedicated top-level Settings field (e.g.
// panelAccess) must prefer the explicit top-level value. Echoing back a
// redacted GET response leaves a stale ProtocolFields[key] that would
// otherwise silently override the top-level change the user requested.
func TestPanelAccessTopLevelOverridesStaleProtocolField(t *testing.T) {
	schemas := []schema.FieldSchema{{Key: "panelAccess", Type: schema.FieldSelect, Scope: "settings", Default: "local"}}
	v := NewSettingsValidationWithFieldSchemas(schemas)
	current := Settings{PanelListen: "127.0.0.1:2096", Mode: "server", PanelAccess: "caddy", WebBasePath: "/x/", Domain: "d.example.com", Email: "e@example.com"}
	current.ProtocolFields = map[string]any{"panelAccess": "caddy"}
	// echo of GET (stale pf.panelAccess=caddy) + user intent top-level direct
	update := Settings{PanelListen: "0.0.0.0:2096", Mode: "server", PanelAccess: "direct", WebBasePath: "/x/", Domain: "d.example.com", Email: "e@example.com"}
	update.ProtocolFields = map[string]any{"panelAccess": "caddy"}
	if err := v.NormalizeAndValidate(&update, current); err != nil {
		t.Fatal(err)
	}
	if update.PanelAccess != "direct" {
		t.Fatalf("expected top-level panelAccess=direct to win, got %q", update.PanelAccess)
	}
}

// ProtocolFields must still be honoured when no dedicated top-level field
// holds a value for the key (the original code path for protocol-only keys).
func TestProtocolFieldValueFallsBackToProtocolFields(t *testing.T) {
	schemas := []schema.FieldSchema{{Key: "customOnly", Type: schema.FieldText, Scope: "settings"}}
	v := NewSettingsValidationWithFieldSchemas(schemas)
	current := Settings{PanelListen: "127.0.0.1:2096", Mode: "server"}
	update := Settings{PanelListen: "127.0.0.1:2096", Mode: "server"}
	update.ProtocolFields = map[string]any{"customOnly": "kept"}
	if err := v.NormalizeAndValidate(&update, current); err != nil {
		t.Fatal(err)
	}
	if got := update.ProtocolFields["customOnly"]; got != "kept" {
		t.Fatalf("expected protocolFields.customOnly=kept, got %v", got)
	}
}
