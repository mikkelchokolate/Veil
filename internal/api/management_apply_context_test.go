package api

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

type fakeFirewallApplier struct {
	enableCalled bool
	applyCalled  bool
	gotRules     []firewall.Rule
	enableErr    error
	applyErr     error
}

func (f *fakeFirewallApplier) EnsureActive() error {
	f.enableCalled = true
	return f.enableErr
}

func (f *fakeFirewallApplier) ApplyRules(rules []firewall.Rule) error {
	f.applyCalled = true
	f.gotRules = rules
	return f.applyErr
}

func TestManagementApplyContextBuildsApplyPlanFromState(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	ctx := NewManagementApplyContext(state)

	plan := ctx.buildApplyPlanLocked()
	if len(plan.Actions) == 0 {
		t.Fatalf("expected apply plan actions, got %+v", plan)
	}
}

func TestReloadPromotedServicesUsesCurrentStateRuntimeCatalog(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	state.applyRoot = t.TempDir()
	state.liveRoot = filepath.Join(state.applyRoot, "live")
	state.inbounds = []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}
	client := &recordingPrivilegedClient{}
	state.privileged = client
	state.privilegedLocal = false

	ctx := NewManagementApplyContext(state)
	results := ctx.reloadPromotedServicesLocked([]string{filepath.Join(state.liveRoot, "hysteria2", "edge.yaml")})
	if len(results) == 0 {
		t.Fatalf("expected service actions, got none")
	}
	if len(client.serviceActions) != 1 {
		t.Fatalf("actions = %+v", client.serviceActions)
	}
	if client.serviceActions[0].Unit != "veil-hysteria2@edge.service" || client.serviceActions[0].Action != privileged.ServiceActionRestart {
		t.Fatalf("unexpected action: %+v", client.serviceActions[0])
	}
}

func TestCheckServiceHealthUsesCurrentStateRuntimeCatalog(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	state.inbounds = []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}
	state.privilegedLocal = true
	ctx := NewManagementApplyContext(state)

	oldChecker := serviceHealthChecker
	serviceHealthChecker = func(serviceName string) ServiceHealthResult {
		return ServiceHealthResult{Name: serviceName, Healthy: true}
	}
	t.Cleanup(func() { serviceHealthChecker = oldChecker })

	results := ctx.checkServiceHealthLocked([]ServiceActionResult{{Name: "veil-hysteria2@edge.service", Success: true}})
	if len(results) != 1 || results[0].Name != "veil-hysteria2@edge.service" || !results[0].Healthy {
		t.Fatalf("unexpected health results: %+v", results)
	}
}

func TestSyncFirewallLockedOpensPanelPortByDefault(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	state.settings.PanelListen = "0.0.0.0:3000"
	state.settings.PanelAccess = "direct"

	fake := &fakeFirewallApplier{}
	old := firewallApplierInstance
	firewallApplierInstance = fake
	t.Cleanup(func() { firewallApplierInstance = old })

	ctx := NewManagementApplyContext(state)
	results := ctx.syncFirewallLocked()

	if len(results) != 1 || !results[0].Success {
		t.Fatalf("expected one successful firewall sync, got %+v", results)
	}
	if !fake.enableCalled {
		t.Fatal("expected EnsureActive to be called")
	}
	if !fake.applyCalled {
		t.Fatal("expected ApplyRules to be called")
	}
	if len(fake.gotRules) == 0 {
		t.Fatal("expected at least one firewall rule")
	}
	found := false
	for _, r := range fake.gotRules {
		if len(r.Args) >= 2 && r.Args[0] == "allow" && r.Args[1] == "3000/tcp" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected panel port rule, got %+v", fake.gotRules)
	}
}

func TestSyncFirewallLockedDisabledBySetting(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	state.settings.PanelListen = "0.0.0.0:3000"
	state.settings.PanelAccess = "direct"
	disabled := false
	state.settings.FirewallManagement = &disabled

	fake := &fakeFirewallApplier{}
	old := firewallApplierInstance
	firewallApplierInstance = fake
	t.Cleanup(func() { firewallApplierInstance = old })

	ctx := NewManagementApplyContext(state)
	results := ctx.syncFirewallLocked()

	if len(results) != 0 {
		t.Fatalf("expected no firewall results when disabled, got %+v", results)
	}
	if fake.enableCalled || fake.applyCalled {
		t.Fatal("expected no firewall calls when disabled")
	}
}

func TestSyncFirewallLockedReportsNonFatalErrors(t *testing.T) {
	state := newManagementState(ServerInfo{Version: "test", Mode: "dev"})
	state.settings.PanelListen = "0.0.0.0:3000"
	state.settings.PanelAccess = "direct"

	fake := &fakeFirewallApplier{enableErr: errors.New("ufw not found")}
	old := firewallApplierInstance
	firewallApplierInstance = fake
	t.Cleanup(func() { firewallApplierInstance = old })

	ctx := NewManagementApplyContext(state)
	results := ctx.syncFirewallLocked()

	if len(results) != 1 || results[0].Success || results[0].Error != "ufw not found" {
		t.Fatalf("expected non-fatal enable error, got %+v", results)
	}
}

func TestRollbackPromotedConfigsDoesNotRestartNewlyAddedInbound(t *testing.T) {
	root := t.TempDir()
	staged := filepath.Join(root, "generated", "hysteria2", "edge.yaml")
	if err := os.MkdirAll(filepath.Dir(staged), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("config"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := &recordingPrivilegedClient{
		statusActiveState: "inactive",
		promoteResult: privileged.PromoteResult{
			BackupID:         "20260608T120000.000000000Z",
			WrittenArtifacts: []string{"hysteria2/edge.yaml"},
		},
	}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: root, Privileged: client})
	state.inbounds = []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}
	ctx := NewManagementApplyContext(state)

	liveFiles, _, records, err := ctx.promoteStagedConfigsLocked([]string{staged})
	if err != nil {
		t.Fatalf("promote staged configs: %v", err)
	}
	if len(liveFiles) != 1 {
		t.Fatalf("expected one live file, got %+v", liveFiles)
	}

	// Simulate a rollback where the newly added config has no previous version
	// to restore; the helper removes it again.
	client.promoteResult = privileged.PromoteResult{
		BackupID:         "20260608T120000.000000000Z",
		WrittenArtifacts: []string{},
	}
	rollbackFiles, rollbackActions := ctx.rollbackPromotedConfigsLocked(records, liveFiles)
	if len(rollbackFiles) != 0 {
		t.Fatalf("expected no rollback files for newly added inbound, got %+v", rollbackFiles)
	}
	for _, action := range rollbackActions {
		if strings.Contains(action.Name, "hysteria2@edge") {
			t.Fatalf("rollback should not restart a newly added inbound that was removed, got %+v", rollbackActions)
		}
	}
}

func TestPromoteStagedConfigsLockedNoOpWhenNothingToDo(t *testing.T) {
	client := &recordingPrivilegedClient{statusActiveState: "inactive"}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: t.TempDir(), Privileged: client})
	ctx := NewManagementApplyContext(state)

	liveFiles, backupFiles, records, err := ctx.promoteStagedConfigsLocked(nil)
	if err != nil {
		t.Fatalf("expected no error when nothing to promote, got %v", err)
	}
	if len(client.promotions) != 0 {
		t.Fatalf("expected no promotion calls, got %+v", client.promotions)
	}
	if len(liveFiles) != 0 || len(backupFiles) != 0 || len(records) != 0 {
		t.Fatalf("expected empty result, got live=%+v backup=%+v records=%+v", liveFiles, backupFiles, records)
	}
}

func TestReloadPromotedServicesStopsOrphansBeforeReloading(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: t.TempDir()})
	state.liveRoot = filepath.Join(state.applyRoot, "live")
	state.inbounds = []Inbound{{Name: "new", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}

	client := &recordingPrivilegedClient{statusActiveState: "inactive"}
	state.privileged = client
	state.privilegedLocal = false

	ctx := NewManagementApplyContext(state)
	state.orphanedUnits = []string{"veil-hysteria2@old.service"}
	liveFiles := []string{filepath.Join(state.liveRoot, "hysteria2", "new.yaml")}

	ctx.reloadPromotedServicesLocked(liveFiles)

	var got []string
	for _, a := range client.serviceActions {
		got = append(got, a.Unit+":"+string(a.Action))
	}
	want := []string{
		"veil-hysteria2@old.service:stop",
		"veil-hysteria2@old.service:disable",
		"veil-hysteria2@new.service:restart",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("service actions = %v, want %v", got, want)
	}
}
