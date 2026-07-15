package systemdunits

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/renderer"
)

func TestNamesIncludesCoreAndProtocolUnits(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("expected managed unit names")
	}
	for _, want := range []string{
		renderer.UnitVeil,
		renderer.UnitHelperService,
		renderer.UnitHelperSocket,
		renderer.UnitBackupService,
		renderer.UnitBackupTimer,
		renderer.UnitWarp,
		renderer.UnitCaddy,
		renderer.UnitHysteria2,
		renderer.UnitOlcrtc,
		renderer.UnitMieru,
	} {
		if !containsString(names, want) {
			t.Fatalf("managed unit names missing %q: %v", want, names)
		}
	}
}

func TestNamesHasNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range Names() {
		if seen[name] {
			t.Fatalf("duplicate unit name %q", name)
		}
		seen[name] = true
	}
}

func TestRenderReturnsContentForAllNames(t *testing.T) {
	units := Render(renderer.SystemdConfig{})
	for _, name := range Names() {
		if strings.TrimSpace(units[name]) == "" {
			t.Fatalf("missing rendered content for %q", name)
		}
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
