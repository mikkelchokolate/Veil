package naiveproxy

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/protocols/schema"
)

// TestFieldSchemaFallbackRootHasNoPersistedDefault locks the contract that the
// fallbackRoot schema entry never carries an env-derived absolute Default:
// the settings normalizer persists f.Default for omitted fields, so a
// DefaultFallbackRoot() value resolved at write time would freeze the install
// layout into state and go stale under VEIL_ETC_DIR/--etc-dir drift. The
// managed root must instead resolve at render time through NaiveFallbackRoot,
// while the Placeholder keeps the effective path discoverable in the UI.
func TestFieldSchemaFallbackRootHasNoPersistedDefault(t *testing.T) {
	p := New()
	check := func(scope string, fields []schema.FieldSchema) {
		t.Helper()
		found := 0
		for _, f := range fields {
			if f.Key != "fallbackRoot" {
				continue
			}
			found++
			if f.Default != nil {
				t.Errorf("%s fallbackRoot Default = %v, want nil so an omitted value is never persisted", scope, f.Default)
			}
			if !strings.Contains(f.Placeholder, DefaultFallbackRoot()) {
				t.Errorf("%s fallbackRoot Placeholder = %q, want it to surface the managed root %q", scope, f.Placeholder, DefaultFallbackRoot())
			}
		}
		if found != 1 {
			t.Errorf("expected exactly one fallbackRoot field in %s schema, got %d", scope, found)
		}
	}
	check("inbound", p.InboundFieldSchema())
	check("settings", p.SettingsFieldSchema())
}
