package olcrtc

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

// TestAutofillPersistsEffectiveKeyInProtocolFields covers audit #122: a bare
// olcRTC Autofill must store the generated key in protocolFields["password"],
// not only in the flat field, so the SPA password input renders the stored
// key (redacted) and Generate remains an explicit rotation.
func TestAutofillPersistsEffectiveKeyInProtocolFields(t *testing.T) {
	filled, err := (Plugin{}).Autofill(model.Inbound{Name: "o", Protocol: "olcrtc", Transport: "udp", Port: 443, Enabled: true})
	if err != nil {
		t.Fatalf("Autofill: %v", err)
	}
	dynamic, _ := filled.ProtocolFields["password"].(string)
	if dynamic == "" || dynamic != filled.Password {
		t.Fatalf("protocolFields[password] = %q, want mirror of flat %q", dynamic, filled.Password)
	}
	if !isOlcrtcKey(dynamic) {
		t.Fatalf("persisted key %q is not a 64-hex olcRTC key", dynamic)
	}
}

// TestAutofillMirrorsFlatKeyIntoUnsetDynamicField ensures an absent/empty or
// still-redacted dynamic field inherits the effective key instead of hiding
// it behind an empty value (audit #122).
func TestAutofillMirrorsFlatKeyIntoUnsetDynamicField(t *testing.T) {
	key := strings.Repeat("c", 64)
	for _, dynamic := range []any{nil, "", veilsettings.RedactedSecret} {
		fields := map[string]any{}
		if dynamic != nil {
			fields["password"] = dynamic
		}
		filled, err := (Plugin{}).Autofill(model.Inbound{
			Name: "o", Protocol: "olcrtc", Transport: "udp", Port: 443, Enabled: true,
			Password: key, ProtocolFields: fields,
		})
		if err != nil {
			t.Fatalf("Autofill(dynamic=%v): %v", dynamic, err)
		}
		if got := filled.ProtocolFields["password"]; got != key {
			t.Fatalf("dynamic=%v: protocolFields[password] = %v, want %q", dynamic, got, key)
		}
	}
}

// TestOlcrtcKeyFallsBackToFlatWhenDynamicUnset ensures an empty or redacted
// dynamic value can never shadow a valid stored flat key (audit #122).
func TestOlcrtcKeyFallsBackToFlatWhenDynamicUnset(t *testing.T) {
	key := strings.Repeat("d", 64)
	for _, dynamic := range []any{"", "   ", veilsettings.RedactedSecret} {
		inbound := model.Inbound{Password: key, ProtocolFields: map[string]any{"password": dynamic}}
		if got := olcrtcKey(inbound); got != key {
			t.Fatalf("dynamic=%q: olcrtcKey = %q, want flat %q", dynamic, got, key)
		}
	}
}

// TestAutofillKeepsExplicitDynamicKey confirms a user-supplied valid dynamic
// key still wins over the flat field and survives the mirror step.
func TestAutofillKeepsExplicitDynamicKey(t *testing.T) {
	flat := strings.Repeat("a", 64)
	dynamic := strings.Repeat("e", 64)
	filled, err := (Plugin{}).Autofill(model.Inbound{
		Name: "o", Protocol: "olcrtc", Transport: "udp", Port: 443, Enabled: true,
		Password:       flat,
		ProtocolFields: map[string]any{"password": dynamic},
	})
	if err != nil {
		t.Fatalf("Autofill: %v", err)
	}
	if filled.Password != dynamic || filled.ProtocolFields["password"] != dynamic {
		t.Fatalf("explicit dynamic key lost: flat=%q dynamic=%v", filled.Password, filled.ProtocolFields["password"])
	}
}
