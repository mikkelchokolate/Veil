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
			err := NewSettingsValidation().NormalizeAndValidate(&tc.settings, Settings{})
			if err == nil || err.Error() != "panelListen and mode are required" {
				t.Fatalf("expected required-field error, got %v", err)
			}
		})
	}
}

func TestSettingsValidationRejectsInvalidPanelAccess(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server", PanelAccess: "invalid"}
	err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{})
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
			if err := NewSettingsValidation().NormalizeAndValidate(&settings, current); err != nil {
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
			err := NewSettingsValidation().NormalizeAndValidate(&tc.settings, Settings{})
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
			err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{})
			if err == nil || err.Error() != tc.errStr {
				t.Fatalf("listen %q: expected %q, got %v", tc.listen, tc.errStr, err)
			}
		})
	}
}

func TestSettingsValidationFallbackRootEscapesVarLibVeil(t *testing.T) {
	cases := []string{
		"../../etc/passwd",
		"/etc/passwd",
		"/var/lib/veil2",
		"/var/lib/veil-evil",
		"/var/lib/veil2/sub",
		"/var/lib",
	}
	for _, root := range cases {
		t.Run(root, func(t *testing.T) {
			settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server", FallbackRoot: root}
			err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{})
			if err == nil || err.Error() != "fallbackRoot must be within /var/lib/veil" {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestSettingsValidationFallsBackToCurrentValues(t *testing.T) {
	current := Settings{
		PanelAccess:     "local",
		WebBasePath:     "/panel/",
		OlcrtcAuth:      "old-auth",
		OlcrtcTransport: "old-transport",
		OlcrtcRoomID:    "old-room",
	}
	update := Settings{PanelListen: "127.0.0.1:2096", Mode: "server"}

	if err := NewSettingsValidation().NormalizeAndValidate(&update, current); err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}

	if update.PanelAccess != current.PanelAccess {
		t.Fatalf("PanelAccess = %q, want %q", update.PanelAccess, current.PanelAccess)
	}
	if update.WebBasePath != current.WebBasePath {
		t.Fatalf("WebBasePath = %q, want %q", update.WebBasePath, current.WebBasePath)
	}
	if update.OlcrtcAuth != current.OlcrtcAuth {
		t.Fatalf("OlcrtcAuth = %q, want %q", update.OlcrtcAuth, current.OlcrtcAuth)
	}
	if update.OlcrtcTransport != current.OlcrtcTransport {
		t.Fatalf("OlcrtcTransport = %q, want %q", update.OlcrtcTransport, current.OlcrtcTransport)
	}
	if update.OlcrtcRoomID != current.OlcrtcRoomID {
		t.Fatalf("OlcrtcRoomID = %q, want %q", update.OlcrtcRoomID, current.OlcrtcRoomID)
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
			if err := NewSettingsValidation().NormalizeAndValidate(&settings, Settings{}); err != nil {
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
	err := NewSettingsValidation().NormalizeAndValidate(&settings, current)
	if err == nil || !strings.Contains(err.Error(), "webBasePath is required for caddy Panel access") {
		t.Fatalf("err = %v", err)
	}
}
