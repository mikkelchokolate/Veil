package cli

import (
	"testing"

	"github.com/veil-panel/veil/internal/installer"
)

func TestSystemdUnitsForPanelCaddyIncludesPanelReverseProxyUnit(t *testing.T) {
	units := systemdUnitsForProfile(installer.RURecommendedProfile{InstallPanelCaddy: true})
	want := []string{"veil.service", "veil-naive.service"}
	if !equalStringSlices(units, want) {
		t.Fatalf("systemdUnitsForProfile = %+v, want %+v", units, want)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
