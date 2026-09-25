package api

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

func TestRedactInboundMasksAllCredentialFields(t *testing.T) {
	in := Inbound{
		Name:              "x",
		Password:          "secret-pw",
		NaivePassword:     "secret-naive",
		Hysteria2Password: "secret-hy2",
		ProtocolFields: map[string]any{
			"password":          "secret-pw",
			"naivePassword":     "secret-naive",
			"hysteria2Password": "secret-hy2",
			"port":              443,
		},
	}
	out := redactInbound(in)
	if out.Password != veilsettings.RedactedSecret {
		t.Errorf("Password not redacted: %q", out.Password)
	}
	if out.NaivePassword != veilsettings.RedactedSecret {
		t.Errorf("NaivePassword not redacted: %q", out.NaivePassword)
	}
	if out.Hysteria2Password != veilsettings.RedactedSecret {
		t.Errorf("Hysteria2Password not redacted: %q", out.Hysteria2Password)
	}
	for _, k := range []string{"password", "naivePassword", "hysteria2Password"} {
		if got := out.ProtocolFields[k]; got != veilsettings.RedactedSecret {
			t.Errorf("ProtocolFields[%s] not redacted: %v", k, got)
		}
	}
	if out.ProtocolFields["port"] != 443 {
		t.Errorf("non-secret field changed: %v", out.ProtocolFields["port"])
	}
	// Input must not be mutated.
	if in.Password != "secret-pw" || in.ProtocolFields["password"] != "secret-pw" {
		t.Errorf("redactInbound mutated its input")
	}
}

func TestRedactInboundKeepsEmptySecretsEmpty(t *testing.T) {
	out := redactInbound(Inbound{Name: "x"})
	if out.Password != "" || out.NaivePassword != "" || out.Hysteria2Password != "" {
		t.Errorf("empty secrets should stay empty, got %+v", out)
	}
}

func TestRedactInboundList(t *testing.T) {
	if redactInboundList(nil) != nil {
		t.Fatal("nil list should stay nil")
	}
	out := redactInboundList([]Inbound{{Name: "a", Password: "p"}, {Name: "b"}})
	if len(out) != 2 || out[0].Password != veilsettings.RedactedSecret || out[1].Password != "" {
		t.Errorf("unexpected list redaction: %+v", out)
	}
}

func TestPreserveRedactedInboundRestoresStoredSecrets(t *testing.T) {
	current := Inbound{
		Name:              "x",
		Password:          "live-pw",
		NaivePassword:     "live-naive",
		Hysteria2Password: "live-hy2",
		ProtocolFields: map[string]any{
			"password":          "live-pw",
			"naivePassword":     "live-naive",
			"hysteria2Password": "live-hy2",
		},
	}
	// A PUT from the panel echoes the redacted GET representation.
	update := Inbound{
		Name:              "x",
		Password:          veilsettings.RedactedSecret,
		NaivePassword:     veilsettings.RedactedSecret,
		Hysteria2Password: veilsettings.RedactedSecret,
		Port:              8443,
		ProtocolFields: map[string]any{
			"password":          veilsettings.RedactedSecret,
			"naivePassword":     veilsettings.RedactedSecret,
			"hysteria2Password": veilsettings.RedactedSecret,
		},
	}
	out := preserveRedactedInbound(update, current)
	if out.Password != "live-pw" || out.NaivePassword != "live-naive" || out.Hysteria2Password != "live-hy2" {
		t.Errorf("flat secrets not preserved: %+v", out)
	}
	for _, k := range []string{"password", "naivePassword", "hysteria2Password"} {
		if got := out.ProtocolFields[k]; got != current.ProtocolFields[k] {
			t.Errorf("ProtocolFields[%s] = %v, want %v", k, got, current.ProtocolFields[k])
		}
	}
	if out.Port != 8443 {
		t.Errorf("non-secret update lost: port = %d", out.Port)
	}
	// Input must not be mutated.
	if update.Password != veilsettings.RedactedSecret {
		t.Errorf("preserveRedactedInbound mutated its input")
	}
}

func TestPreserveRedactedInboundKeepsNewSecrets(t *testing.T) {
	current := Inbound{Name: "x", Password: "old"}
	update := Inbound{Name: "x", Password: "new-secret"}
	out := preserveRedactedInbound(update, current)
	if out.Password != "new-secret" {
		t.Errorf("real new secret replaced: %q", out.Password)
	}
}

func TestPreserveRedactedInboundFallsBackToFlatForMissingMapKey(t *testing.T) {
	// ProtocolFields password keys mirror flat fields; when the map key is
	// absent on the stored record, the flat value must be preserved instead of
	// writing the sentinel.
	current := Inbound{Name: "x", Password: "flat-live"}
	update := Inbound{
		Name: "x",
		ProtocolFields: map[string]any{
			"password": veilsettings.RedactedSecret,
		},
	}
	out := preserveRedactedInbound(update, current)
	if got := out.ProtocolFields["password"]; got != "flat-live" {
		t.Errorf("ProtocolFields[password] = %v, want flat fallback %q", got, "flat-live")
	}
}

func TestRedactInboundMasksProfilePasswords(t *testing.T) {
	in := Inbound{
		Name: "x",
		Profiles: []model.ClientProfile{
			{Name: "alice", Password: "profile-secret", Enabled: true},
			{Name: "bob", Password: "", Enabled: false},
		},
	}
	out := redactInbound(in)
	if len(out.Profiles) != 2 {
		t.Fatalf("profiles lost: %+v", out.Profiles)
	}
	if out.Profiles[0].Password != veilsettings.RedactedSecret {
		t.Errorf("profile password not redacted: %q", out.Profiles[0].Password)
	}
	if out.Profiles[1].Password != "" {
		t.Errorf("empty profile password should stay empty: %q", out.Profiles[1].Password)
	}
	// Input must not be mutated.
	if in.Profiles[0].Password != "profile-secret" {
		t.Errorf("redactInbound mutated its input")
	}
}

func TestPreserveRedactedInboundRestoresProfilePasswords(t *testing.T) {
	current := Inbound{
		Name: "x",
		Profiles: []model.ClientProfile{
			{Name: "alice", Password: "live-alice", Enabled: true},
			{Name: "bob", Password: "live-bob", Enabled: false},
		},
	}
	// The panel echoes the redacted GET representation on save.
	update := Inbound{
		Name: "x",
		Profiles: []model.ClientProfile{
			{Name: "alice", Password: veilsettings.RedactedSecret, Enabled: true},
			{Name: "bob", Password: veilsettings.RedactedSecret, Enabled: false},
		},
	}
	out := preserveRedactedInbound(update, current)
	if out.Profiles[0].Password != "live-alice" || out.Profiles[1].Password != "live-bob" {
		t.Errorf("profile passwords not preserved: %+v", out.Profiles)
	}
	// Input must not be mutated.
	if update.Profiles[0].Password != veilsettings.RedactedSecret {
		t.Errorf("preserveRedactedInbound mutated its input")
	}
}

func TestPreserveRedactedInboundKeepsNewProfilePassword(t *testing.T) {
	current := Inbound{Name: "x", Profiles: []model.ClientProfile{{Name: "alice", Password: "old"}}}
	update := Inbound{Name: "x", Profiles: []model.ClientProfile{{Name: "alice", Password: "new-profile-secret"}}}
	out := preserveRedactedInbound(update, current)
	if out.Profiles[0].Password != "new-profile-secret" {
		t.Errorf("real new profile secret replaced: %q", out.Profiles[0].Password)
	}
}

// TestPreserveRedactedInboundMatchesProfilesByName (#1051): the stored
// password must follow the profile NAME, not its position. Reordering or
// deleting a middle profile must not graft another profile's credential onto
// the sentinel-echoed entry.
func TestPreserveRedactedInboundMatchesProfilesByName(t *testing.T) {
	current := Inbound{
		Name: "x",
		Profiles: []model.ClientProfile{
			{Name: "alice", Password: "live-alice"},
			{Name: "bob", Password: "live-bob"},
			{Name: "carol", Password: "live-carol"},
		},
	}
	// bob deleted; alice and carol swapped order — every surviving name must
	// recover ITS OWN stored password, not the one sitting at its index.
	update := Inbound{
		Name: "x",
		Profiles: []model.ClientProfile{
			{Name: "carol", Password: veilsettings.RedactedSecret},
			{Name: "alice", Password: veilsettings.RedactedSecret},
		},
	}
	out := preserveRedactedInbound(update, current)
	if out.Profiles[0].Password != "live-carol" {
		t.Errorf("carol got %q — positional restore would have grafted live-alice", out.Profiles[0].Password)
	}
	if out.Profiles[1].Password != "live-alice" {
		t.Errorf("alice got %q — positional restore would have grafted live-bob", out.Profiles[1].Password)
	}
}

// TestPreserveRedactedInboundNewProfileSentinelGetsEmpty (#1051): a profile
// that does not exist in the stored config has no stored credential to
// restore — the sentinel must degrade to empty rather than borrowing the
// password positionally adjacent in the stored list.
func TestPreserveRedactedInboundNewProfileSentinelGetsEmpty(t *testing.T) {
	current := Inbound{
		Name:     "x",
		Profiles: []model.ClientProfile{{Name: "alice", Password: "live-alice"}},
	}
	update := Inbound{
		Name: "x",
		Profiles: []model.ClientProfile{
			{Name: "mallory", Password: veilsettings.RedactedSecret},
		},
	}
	out := preserveRedactedInbound(update, current)
	if out.Profiles[0].Password != "" {
		t.Errorf("unknown profile gained credential %q", out.Profiles[0].Password)
	}
}

// TestPreserveRedactedInboundDuplicateNamesRestoreInOrder (#1051): profiles
// are allowed to share a display name; the stored passwords for duplicate
// names must be consumed in order, exactly like the old positional restore
// behaved for the common non-reordered case.
func TestPreserveRedactedInboundDuplicateNamesRestoreInOrder(t *testing.T) {
	current := Inbound{
		Name: "x",
		Profiles: []model.ClientProfile{
			{Name: "shared", Password: "live-first"},
			{Name: "shared", Password: "live-second"},
		},
	}
	update := Inbound{
		Name: "x",
		Profiles: []model.ClientProfile{
			{Name: "shared", Password: veilsettings.RedactedSecret},
			{Name: "shared", Password: veilsettings.RedactedSecret},
		},
	}
	out := preserveRedactedInbound(update, current)
	if out.Profiles[0].Password != "live-first" || out.Profiles[1].Password != "live-second" {
		t.Errorf("duplicate-name restore order wrong: %+v", out.Profiles)
	}
}
