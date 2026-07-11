package settings

import (
	"strings"
	"testing"
)

func TestSettingsValidationRejectsUnsafeWebBasePaths(t *testing.T) {
	for _, value := range []string{
		"panel?debug",
		"panel#fragment",
		"panel'break",
		"panel\\admin",
		"panel//admin",
		"panel/../admin",
	} {
		t.Run(value, func(t *testing.T) {
			settings := Settings{
				PanelListen: "127.0.0.1:2096",
				Mode:        "server",
				WebBasePath: value,
			}
			err := NewSettingsValidationWithFieldSchemas(testSettingsFieldSchemas()).NormalizeAndValidate(&settings, Settings{})
			if err == nil || !strings.Contains(err.Error(), "webBasePath:") {
				t.Fatalf("WebBasePath %q: expected validation error, got %v", value, err)
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
	if err == nil || !strings.Contains(err.Error(), "webBasePath:") {
		t.Fatalf("expected inherited unsafe WebBasePath to be rejected, got %v", err)
	}
}
