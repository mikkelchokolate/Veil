package settings

import (
	"strings"
	"testing"
)

func TestSettingsValidationRejectsMissingRequiredFields(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
	}{
		{
			name:     "missing panelListen",
			settings: Settings{Mode: "server"},
		},
		{
			name:     "missing mode",
			settings: Settings{PanelListen: "127.0.0.1:2096"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&tc.settings, Settings{})
			if err == nil || err.Error() != "panelListen and mode are required" {
				t.Fatalf("expected required-field error, got %v", err)
			}
		})
	}
}

func TestSettingsValidationRejectsInvalidPanelAccess(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server", PanelAccess: "invalid"}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
	if err == nil || err.Error() != "panel access must be direct, local, or caddy" {
		t.Fatalf("err = %v", err)
	}
}

func TestSettingsValidationAcceptsValidPanelAccessValues(t *testing.T) {
	for _, access := range []string{"direct", "local", "caddy"} {
		t.Run(access, func(t *testing.T) {
			settings := Settings{
				PanelListen: "127.0.0.1:2096",
				Mode:        "server",
				PanelAccess: access,
			}
			current := Settings{}
			if access == "caddy" {
				settings.WebBasePath = "/panel/"
				settings.Domain = "example.com"
				settings.Email = "admin@example.com"
			}
			if err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, current); err != nil {
				t.Fatalf("access %q: %v", access, err)
			}
		})
	}
}

func TestSettingsValidationDomainAndEmailErrors(t *testing.T) {
	tests := []struct {
		name            string
		settings        Settings
		wantErrContains string
	}{
		{
			name:            "domain missing dot",
			settings:        Settings{PanelListen: "127.0.0.1:2096", Mode: "server", Domain: "example"},
			wantErrContains: "domain: domain must include at least one dot",
		},
		{
			name:            "domain contains URL scheme",
			settings:        Settings{PanelListen: "127.0.0.1:2096", Mode: "server", Domain: "https://example.com"},
			wantErrContains: "domain: domain must be a hostname, not a URL",
		},
		{
			name:            "invalid email",
			settings:        Settings{PanelListen: "127.0.0.1:2096", Mode: "server", Email: "not-an-email"},
			wantErrContains: "email: invalid email: mail: missing '@' or angle-addr",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&tc.settings, Settings{})
			if err == nil || err.Error() != tc.wantErrContains {
				t.Fatalf("expected %q, got %v", tc.wantErrContains, err)
			}
		})
	}
}

func TestSettingsValidationPanelListenErrors(t *testing.T) {
	tests := []struct {
		listen string
		errStr string
	}{
		{":2096", "panelListen must be host:port"},
		{"127.0.0.1:", "panelListen port must be a valid integer between 1 and 65535"},
	}

	for _, tc := range tests {
		t.Run(tc.listen, func(t *testing.T) {
			settings := Settings{PanelListen: tc.listen, Mode: "server"}
			err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
			if err == nil || err.Error() != tc.errStr {
				t.Fatalf("listen %q: expected %q, got %v", tc.listen, tc.errStr, err)
			}
		})
	}
}

func TestSettingsValidationFallbackRootEscapesVarLibVeil(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server", FallbackRoot: "../../etc/passwd"}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
	if err == nil || err.Error() != "fallbackRoot must be within /var/lib/veil" {
		t.Fatalf("err = %v", err)
	}
}

func TestSettingsValidationFallsBackToCurrentValues(t *testing.T) {
	current := Settings{
		PanelAccess: "local",
		WebBasePath: "/panel/",
		ProtocolFields: map[string]any{
			"olcrtcAuth":      "old-auth",
			"olcrtcTransport": "old-transport",
			"olcrtcRoomID":    "old-room",
		},
	}
	update := Settings{PanelListen: "127.0.0.1:2096", Mode: "server"}

	if err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&update, current); err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}

	if update.PanelAccess != current.PanelAccess {
		t.Fatalf("PanelAccess = %q, want %q", update.PanelAccess, current.PanelAccess)
	}
	if update.WebBasePath != current.WebBasePath {
		t.Fatalf("WebBasePath = %q, want %q", update.WebBasePath, current.WebBasePath)
	}
	if update.ProtocolFields["olcrtcAuth"] != "old-auth" {
		t.Fatalf("olcrtcAuth = %v, want %q", update.ProtocolFields["olcrtcAuth"], "old-auth")
	}
	if update.ProtocolFields["olcrtcTransport"] != "old-transport" {
		t.Fatalf("olcrtcTransport = %v, want %q", update.ProtocolFields["olcrtcTransport"], "old-transport")
	}
	if update.ProtocolFields["olcrtcRoomID"] != "old-room" {
		t.Fatalf("olcrtcRoomID = %v, want %q", update.ProtocolFields["olcrtcRoomID"], "old-room")
	}
}

func TestSettingsValidationNormalizesWebBasePathFromUpdate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"panel", "/panel/"},
		{"/panel", "/panel/"},
		{"panel/", "/panel/"},
		{"/panel/", "/panel/"},
		{"  panel  ", "/panel/"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server", WebBasePath: tc.input}
			if err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{}); err != nil {
				t.Fatalf("NormalizeAndValidate: %v", err)
			}
			if settings.WebBasePath != tc.want {
				t.Fatalf("WebBasePath = %q, want %q", settings.WebBasePath, tc.want)
			}
		})
	}
}

func TestNormalizeWebBasePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"/", ""},
		{"panel", "/panel/"},
		{"/panel", "/panel/"},
		{"panel/", "/panel/"},
		{"/panel/", "/panel/"},
		{"  panel  ", "/panel/"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			if got := NormalizeWebBasePath(tc.input); got != tc.want {
				t.Fatalf("NormalizeWebBasePath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSettingsValidationCaddyRequiresWebBasePathEvenFromCurrent(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server", PanelAccess: "caddy"}
	current := Settings{}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, current)
	if err == nil || !strings.Contains(err.Error(), "webBasePath is required for caddy Panel access") {
		t.Fatalf("err = %v", err)
	}
}

// TestSettingsValidationDropsUnknownProtocolKeys guards against persisting the
// redaction sentinel (or any value) under keys no settings-scoped schema
// declares: unknown keys are never consumed by renderers and must not
// accumulate in state.
func TestSettingsValidationDropsUnknownProtocolKeys(t *testing.T) {
	settings := Settings{
		PanelListen: "127.0.0.1:2096",
		Mode:        "server",
		ProtocolFields: map[string]any{
			"bogusKey":      RedactedSecret,
			"naiveUsername": "veil",
		},
	}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
	if err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
	if _, ok := settings.ProtocolFields["bogusKey"]; ok {
		t.Fatalf("unknown key was persisted: %+v", settings.ProtocolFields)
	}
	if settings.ProtocolFields["naiveUsername"] != "veil" {
		t.Fatalf("known key was dropped: %+v", settings.ProtocolFields)
	}
}

// TestSettingsValidationRejectsNonStringPasswordValue guards the FieldPassword
// guard: a non-string value (e.g. an object) must be rejected outright instead
// of being persisted and silently dropped by renderers.
func TestSettingsValidationRejectsNonStringPasswordValue(t *testing.T) {
	settings := Settings{
		PanelListen: "127.0.0.1:2096",
		Mode:        "server",
		ProtocolFields: map[string]any{
			"naivePassword": map[string]any{"x": 1},
		},
	}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
	if err == nil || !strings.Contains(err.Error(), "naivePassword must be a string") {
		t.Fatalf("err = %v", err)
	}
}

// TestSettingsValidationRejectsNullPasswordValue covers the explicit JSON null
// case: a key present with a nil value must be rejected, not silently
// persisted (it would drop the secret from rendered configs).
func TestSettingsValidationRejectsNullPasswordValue(t *testing.T) {
	settings := Settings{
		PanelListen:    "127.0.0.1:2096",
		Mode:           "server",
		ProtocolFields: map[string]any{"naivePassword": nil},
	}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
	if err == nil || !strings.Contains(err.Error(), "naivePassword must be a string") {
		t.Fatalf("err = %v", err)
	}
}

// TestSettingsValidationRejectsUnknownSelectValue guards FieldSelect fields:
// a value outside the declared options must be rejected at save time instead
// of being caught later at apply/live validation.
func TestSettingsValidationRejectsUnknownSelectValue(t *testing.T) {
	settings := Settings{
		PanelListen: "127.0.0.1:2096",
		Mode:        "server",
		ProtocolFields: map[string]any{
			"olcrtcAuth": "datachanne", // typo, not a real provider
		},
	}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
	if err == nil || !strings.Contains(err.Error(), "olcrtcAuth must be one of") {
		t.Fatalf("err = %v", err)
	}
}

// TestSettingsValidationAcceptsKnownSelectValue ensures valid select values
// still pass through untouched.
func TestSettingsValidationAcceptsKnownSelectValue(t *testing.T) {
	settings := Settings{
		PanelListen: "127.0.0.1:2096",
		Mode:        "server",
		ProtocolFields: map[string]any{
			"olcrtcAuth":      "telemost",
			"olcrtcTransport": "vp8channel",
		},
	}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
}

// TestSettingsValidationAcceptsZeroFlatBoolViaProtocolFields covers the
// flat-vs-protocolFields precedence fix: a zero flat bool (client that does
// not send the flat copy, e.g. the legacy panel) must not override a
// protocolFields value.
func TestSettingsValidationAcceptsZeroFlatBoolViaProtocolFields(t *testing.T) {
	settings := Settings{
		PanelListen: "127.0.0.1:2096",
		Mode:        "server",
		// hysteria2Insecure flat is absent -> decodes to false, but the
		// protocolFields copy carries the real value.
		ProtocolFields: map[string]any{
			"hysteria2Insecure": true,
		},
	}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if settings.Hysteria2Insecure != true {
		t.Fatalf("flat false overrode protocolFields true: %+v", settings)
	}
	if settings.ProtocolFields["hysteria2Insecure"] != true {
		t.Fatalf("protocolFields hysteria2Insecure = %v, want true", settings.ProtocolFields["hysteria2Insecure"])
	}
}
