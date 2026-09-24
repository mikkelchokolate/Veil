package settings

import "testing"

func TestSettingsValidationRejectsUnsafeWebBasePaths(t *testing.T) {
	tests := []struct {
		value   string
		wantErr string
	}{
		{"panel?debug", `webBasePath: segment "panel?debug" contains unsupported character '?'`},
		{"panel#fragment", `webBasePath: segment "panel#fragment" contains unsupported character '#'`},
		{"panel'break", `webBasePath: segment "panel'break" contains unsupported character '\''`},
		{"panel\\admin", `webBasePath: segment "panel\\admin" contains unsupported character '\\'`},
		{"panel//admin", `webBasePath: must not contain empty, '.' or '..' segments`},
		{"panel/../admin", `webBasePath: must not contain empty, '.' or '..' segments`},
	}
	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			settings := Settings{
				PanelListen: "127.0.0.1:2096",
				Mode:        "server",
				WebBasePath: tc.value,
			}
			err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("WebBasePath %q: expected %q, got %v", tc.value, tc.wantErr, err)
			}
		})
	}
}

// TestSettingsValidationRejectsReservedWebBasePathFirstSegments locks the
// mount/mux collision guard (#765): a webBasePath whose first segment equals
// a root-mux mount would shadow or hide that endpoint.
func TestSettingsValidationRejectsReservedWebBasePathFirstSegments(t *testing.T) {
	tests := []struct {
		value   string
		wantErr string
	}{
		{"/api/", `webBasePath: first segment "api" is reserved for the management API`},
		{"metrics", `webBasePath: first segment "metrics" is reserved for the Prometheus metrics endpoint`},
		{"healthz", `webBasePath: first segment "healthz" is reserved for the health check endpoint`},
		{"livez", `webBasePath: first segment "livez" is reserved for the liveness endpoint`},
		{"readyz", `webBasePath: first segment "readyz" is reserved for the readiness endpoint`},
		{"assets", `webBasePath: first segment "assets" is reserved for the static asset mount`},
		{"s/panel", `webBasePath: first segment "s" is reserved for public subscription links`},
	}
	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			settings := Settings{
				PanelListen: "127.0.0.1:2096",
				Mode:        "server",
				WebBasePath: tc.value,
			}
			err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("WebBasePath %q: expected %q, got %v", tc.value, tc.wantErr, err)
			}
		})
	}
}

func TestSettingsValidationStoresRootWebBasePathAsEmpty(t *testing.T) {
	settings := Settings{
		PanelListen: "127.0.0.1:2096",
		Mode:        "server",
		WebBasePath: "/",
	}
	if err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{}); err != nil {
		t.Fatalf("NormalizeAndValidate: %v", err)
	}
	if settings.WebBasePath != "" {
		t.Fatalf("WebBasePath = %q, want empty root representation", settings.WebBasePath)
	}
}

func TestSettingsValidationRejectsUnsafeInheritedWebBasePath(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "server"}
	current := Settings{WebBasePath: "panel'</script>"}
	err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, current)
	wantErr := `webBasePath: segment "panel'<" contains unsupported character '\''`
	if err == nil || err.Error() != wantErr {
		t.Fatalf("expected %q, got %v", wantErr, err)
	}
}
