package api

import (
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func TestUnitForArtifactIDMapsWarpConfigToWarpUnit(t *testing.T) {
	unit, ok := UnitForArtifactID("sing-box/warp.json")
	if !ok || unit != "veil-warp.service" {
		t.Fatalf("UnitForArtifactID(sing-box/warp.json) = %q %v, want veil-warp.service true", unit, ok)
	}
}

func TestPromoteRemovesWarpConfigAndStopsUnitWhenWarpDisabledAndActive(t *testing.T) {
	client := &recordingPrivilegedClient{
		// recordingPrivilegedClient.ServiceStatus reports every queried unit as
		// active, modelling a running veil-warp.service.
		promoteResult: privileged.PromoteResult{
			BackupID:         "20260608T120000.000000000Z",
			RemovedArtifacts: []string{"sing-box/warp.json"},
		},
	}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: t.TempDir(), Privileged: client})
	state.warp = WarpConfig{Enabled: false}
	ctx := NewManagementApplyContext(state)

	if _, _, _, err := ctx.promoteStagedConfigsLocked(nil); err != nil {
		t.Fatalf("promote staged configs: %v", err)
	}
	if len(client.promotions) != 1 {
		t.Fatalf("promotions = %+v", client.promotions)
	}
	if !reflect.DeepEqual(client.promotions[0].RemoveArtifactIDs, []string{"sing-box/warp.json"}) {
		t.Fatalf("remove artifact ids = %+v, want [sing-box/warp.json]", client.promotions[0].RemoveArtifactIDs)
	}

	ctx.reloadPromotedServicesLocked(nil)
	wantActions := []privileged.ServiceActionRequest{
		{Unit: "veil-warp.service", Action: privileged.ServiceActionStop},
		{Unit: "veil-warp.service", Action: privileged.ServiceActionDisable},
	}
	if !reflect.DeepEqual(client.serviceActions, wantActions) {
		t.Fatalf("service actions = %+v, want %+v", client.serviceActions, wantActions)
	}
}

func TestPromoteDoesNotTouchWarpWhenDisabledAndUnitInactive(t *testing.T) {
	client := &recordingPrivilegedClient{
		statusActiveState: "inactive",
		promoteResult:     privileged.PromoteResult{BackupID: "20260608T120000.000000000Z"},
	}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: t.TempDir(), Privileged: client})
	state.warp = WarpConfig{Enabled: false, PrivateKey: "provisioned-key"}
	ctx := NewManagementApplyContext(state)

	if _, _, _, err := ctx.promoteStagedConfigsLocked(nil); err != nil {
		t.Fatalf("promote staged configs: %v", err)
	}
	for _, id := range client.promotions[0].RemoveArtifactIDs {
		if id == "sing-box/warp.json" {
			t.Fatalf("inactive WARP unit must not be torn down: %+v", client.promotions[0].RemoveArtifactIDs)
		}
	}
	ctx.reloadPromotedServicesLocked(nil)
	for _, action := range client.serviceActions {
		if action.Unit == "veil-warp.service" {
			t.Fatalf("inactive WARP unit must not receive service actions: %+v", client.serviceActions)
		}
	}
}

func TestPromoteDoesNotRemoveWarpConfigWhenWarpEnabled(t *testing.T) {
	client := &recordingPrivilegedClient{
		promoteResult: privileged.PromoteResult{BackupID: "20260608T120000.000000000Z"},
	}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: t.TempDir(), Privileged: client})
	state.warp = WarpConfig{Enabled: true}
	ctx := NewManagementApplyContext(state)

	if _, _, _, err := ctx.promoteStagedConfigsLocked(nil); err != nil {
		t.Fatalf("promote staged configs: %v", err)
	}
	if len(client.promotions) != 1 {
		t.Fatalf("promotions = %+v", client.promotions)
	}
	for _, id := range client.promotions[0].RemoveArtifactIDs {
		if id == "sing-box/warp.json" {
			t.Fatalf("warp config must not be removed while WARP is enabled: %+v", client.promotions[0].RemoveArtifactIDs)
		}
	}
}
