package api

import "testing"

func TestBuildManagementSnapshotCopiesMutableSlices(t *testing.T) {
	inbounds := []Inbound{{Name: "naive"}}
	rules := []RoutingRule{{Name: "rule"}}
	snapshot := BuildManagementSnapshot(ManagementSnapshotInput{
		Settings:      Settings{},
		Inbounds:      inbounds,
		Rules:         rules,
		RoutingPreset: "preset",
		RoutingSource: RoutingSource{Repository: "repo"},
		Warp:          WarpConfig{Enabled: true},
	})
	inbounds[0].Name = "changed"
	rules[0].Name = "changed"
	if snapshot.Inbounds[0].Name != "naive" || snapshot.Rules[0].Name != "rule" {
		t.Fatalf("snapshot did not copy slices: %+v", snapshot)
	}
}
