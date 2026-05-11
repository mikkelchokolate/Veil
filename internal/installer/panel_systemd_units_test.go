package installer

import "testing"

func TestPanelSystemdUnitsIncludesPanelReverseProxyUnit(t *testing.T) {
	units := PanelSystemdUnits(RURecommendedProfile{InstallPanelCaddy: true})
	want := []string{"veil.service", "veil-naive.service"}
	if !equalStringSlices(units, want) {
		t.Fatalf("PanelSystemdUnits = %+v, want %+v", units, want)
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
