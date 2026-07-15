package repair

import (
	"slices"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/api"
)

func TestRuntimeUnitNamesForStateUsesPassedSnapshot(t *testing.T) {
	units := runtimeUnitNamesForState(api.Settings{}, []api.Inbound{
		{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
	}, api.WarpConfig{})

	if !slices.Contains(units, "veil-hysteria2@edge.service") {
		t.Fatalf("expected per-inbound hysteria2 unit, got %+v", units)
	}
	if slices.Contains(units, "veil-hysteria2@.service") {
		t.Fatalf("runtimeUnitNamesForState should not use fallback template as the active unit: %+v", units)
	}
}

func TestRuntimeUnitNamesForStateIncludesWarpWhenEnabled(t *testing.T) {
	units := runtimeUnitNamesForState(api.Settings{}, nil, api.WarpConfig{Enabled: true})
	if !slices.Contains(units, "veil-warp.service") {
		t.Fatalf("expected WARP unit, got %+v", units)
	}
}
