package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestApplyManagementSnapshotKeepsDefaultsForMissingOptionalSections(t *testing.T) {
	state := &managementState{
		settings:      Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"},
		inbounds:      []Inbound{{Name: "default"}},
		rules:         []RoutingRule{{Name: "default-rule"}},
		routingPreset: "default-preset",
		routingSource: RoutingSource{Repository: "default-repo"},
		warp:          WarpConfig{Endpoint: "default-endpoint"},
	}

	ApplyManagementSnapshot(state, managementSnapshot{Settings: Settings{PanelListen: "0.0.0.0:2096", Mode: "dev"}})

	body, err := json.Marshal(state.settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if strings.Contains(string(body), `"stack"`) {
		t.Fatalf("management state should not emit legacy stack after applying new snapshot: %s", body)
	}
	if state.inbounds[0].Name != "default" || state.rules[0].Name != "default-rule" || state.routingPreset != "default-preset" || state.routingSource.Repository != "default-repo" || state.warp.Endpoint != "default-endpoint" {
		t.Fatalf("missing optional sections should preserve defaults: %+v", state)
	}
}
