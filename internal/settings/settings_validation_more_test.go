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
