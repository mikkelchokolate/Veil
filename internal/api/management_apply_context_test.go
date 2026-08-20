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
	results := ctx.reloadPromotedServices([]string{filepath.Join(state.liveRoot, "hysteria2", "edge.yaml")})
	if len(results) == 0 {
		t.Fatalf("expected service actions, got none")
	}
	if len(client.serviceActions) != 2 {
		t.Fatalf("actions = %+v", client.serviceActions)
	}
	if client.serviceActions[0].Unit != "veil-hysteria2@edge.service" || client.serviceActions[0].Action != privileged.ServiceActionRestart {
		t.Fatalf("unexpected action: %+v", client.serviceActions[0])
	}
	// Boot persistence: the instance unit must also be enabled (audit #117).
	if client.serviceActions[1].Unit != "veil-hysteria2@edge.service" || client.serviceActions[1].Action != privileged.ServiceActionEnable {
		t.Fatalf("expected enable after restart for boot persistence: %+v", client.serviceActions)
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

	results := ctx.checkServiceHealth([]ServiceActionResult{{Name: "veil-hysteria2@edge.service", Command: []string{"systemctl", "restart", "veil-hysteria2@edge.service"}, Success: true}})
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
	results := ctx.syncFirewall()

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
	results := ctx.syncFirewall()

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
	results := ctx.syncFirewall()

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

	liveFiles, _, records, err := ctx.promoteStagedConfigs([]string{staged})
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
	rollbackFiles, rollbackActions := ctx.rollbackPromotedConfigs(records, liveFiles)
	if len(rollbackFiles) != 0 {
		t.Fatalf("expected no rollback files for newly added inbound, got %+v", rollbackFiles)
	}
	for _, action := range rollbackActions {
		if strings.Contains(action.Name, "hysteria2@edge") && action.Success {
			// A stop/disable of the removed unit is correct; a start is not.
			cmd := strings.Join(action.Command, " ")
			if strings.Contains(cmd, "start") || strings.Contains(cmd, "enable") {
				t.Fatalf("rollback should not restart a newly added inbound that was removed, got %+v", rollbackActions)
			}
		}
	}
}

// TestRollbackStopsUnitWhoseConfigWasNewlyAdded covers the health-failure
// rollback path: a brand-new inbound was promoted and started, a later health
// check failed, and restore removed its config (no previous version existed).
// The rollback must stop+disable that unit rather than leaving it running
// against a now-deleted config.
func TestRollbackStopsUnitWhoseConfigWasNewlyAdded(t *testing.T) {
	root := t.TempDir()
	liveRoot := filepath.Join(root, "live")
	livePath := filepath.Join(liveRoot, "hysteria2", "edge.yaml")
	client := &recordingPrivilegedClient{
		statusActiveState: "inactive",
		promoteResult: privileged.PromoteResult{
			BackupID:         "20260716T120000.000000000Z",
			WrittenArtifacts: []string{"hysteria2/edge.yaml"},
		},
	}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: root, LiveRoot: liveRoot, Privileged: client})
	state.inbounds = []Inbound{{Name: "edge", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}
	ctx := NewManagementApplyContext(state)

	// Records mirror what promoteStagedConfigs produced: the artifact was
	// written live (so it is in liveFiles), and it had no previous version, so
	// the restore below will NOT return it in WrittenArtifacts.
	records := []livePromotionRecord{{
		ArtifactID: "hysteria2/edge.yaml",
		BackupID:   "20260716T120000.000000000Z",
		LivePath:   livePath,
	}}
	liveFiles := []string{livePath}

	// Restore returns nothing: the new config had no previous version to
	// restore, so the helper deleted it.
	client.promoteResult = privileged.PromoteResult{
		BackupID:         "20260716T120000.000000000Z",
		WrittenArtifacts: []string{},
	}

	_, _ = ctx.rollbackPromotedConfigs(records, liveFiles)

	var got []string
	for _, a := range client.serviceActions {
		got = append(got, a.Unit+":"+string(a.Action))
	}
	// The unit must be stopped and disabled, and never started/enabled.
	stopped, disabled, startedOrEnabled := false, false, false
	for _, a := range client.serviceActions {
		if a.Unit != "veil-hysteria2@edge.service" {
			continue
		}
		switch a.Action {
		case privileged.ServiceActionStop:
			stopped = true
		case privileged.ServiceActionDisable:
			disabled = true
		case privileged.ServiceActionStart, privileged.ServiceActionEnable:
			startedOrEnabled = true
		}
	}
	if startedOrEnabled {
		t.Fatalf("rollback must not start/enable a removed new inbound, actions=%v", got)
	}
	if !stopped || !disabled {
		t.Fatalf("rollback must stop+disable the removed new inbound's unit, actions=%v", got)
	}
}

func TestRollbackStopsSingletonBeforeRestoringLegacyCaddy(t *testing.T) {
	root := t.TempDir()
	liveRoot := filepath.Join(root, "live")
	legacyPath := filepath.Join(liveRoot, "caddy", "legacy.Caddyfile")
	client := &recordingPrivilegedClient{
		statusActiveState: "inactive",
		promoteResult: privileged.PromoteResult{
			BackupID:         "20260716T120000.000000000Z",
			WrittenArtifacts: []string{"caddy/legacy.Caddyfile"},
		},
	}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: root, LiveRoot: liveRoot, Privileged: client})
	state.settings = Settings{PanelAccess: "caddy"}
	ctx := NewManagementApplyContext(state)
	records := []livePromotionRecord{{
		LivePath:    legacyPath,
		HadPrevious: true,
		ArtifactID:  "caddy/legacy.Caddyfile",
		BackupID:    "20260716T120000.000000000Z",
	}}
	newConfig := filepath.Join(liveRoot, "caddy", "config.json")

	_, _ = ctx.rollbackPromotedConfigs(records, []string{newConfig})

	var got []string
	for _, action := range client.serviceActions {
		got = append(got, action.Unit+":"+string(action.Action))
	}
	want := []string{
		"veil-caddy.service:stop",
		"veil-caddy.service:disable",
		"veil-caddy@legacy.service:enable",
		"veil-caddy@legacy.service:start",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rollback service actions = %v, want %v", got, want)
	}
}

func TestPromoteStagedConfigsLockedNoOpWhenNothingToDo(t *testing.T) {
	client := &recordingPrivilegedClient{statusActiveState: "inactive"}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: t.TempDir(), Privileged: client})
	ctx := NewManagementApplyContext(state)

	liveFiles, backupFiles, records, err := ctx.promoteStagedConfigs(nil)
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

func TestReloadPromotedServicesStopsLegacyCaddyBeforeStartingSingleton(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: t.TempDir()})
	state.liveRoot = filepath.Join(state.applyRoot, "live")
	state.settings = Settings{PanelAccess: "caddy"}
	state.orphanedUnits = []string{"veil-caddy@legacy.service"}
	client := &recordingPrivilegedClient{statusActiveState: "inactive"}
	state.privileged = client
	state.privilegedLocal = false

	caddyPath := filepath.Join(state.liveRoot, "caddy", "config.json")
	if err := os.MkdirAll(filepath.Dir(caddyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(caddyPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	oldLoader := caddyAdminLoader
	caddyAdminLoader = func([]byte) error { return errors.New("admin unavailable") }
	defer func() { caddyAdminLoader = oldLoader }()

	ctx := NewManagementApplyContext(state)
	ctx.reloadPromotedServices([]string{caddyPath})

	var got []string
	for _, action := range client.serviceActions {
		got = append(got, action.Unit+":"+string(action.Action))
	}
	want := []string{
		"veil-caddy@legacy.service:stop",
		"veil-caddy@legacy.service:disable",
		"veil-caddy.service:reload",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("service actions = %v, want %v", got, want)
	}
}

func TestPromoteStagedConfigsCollectsEnabledOrphanTemplateUnits(t *testing.T) {
	wants := t.TempDir()
	if err := os.WriteFile(filepath.Join(wants, "veil-hysteria2@hy2-auto.service"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wants, "veil-hysteria2@sfhgs.service"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	client := &recordingPrivilegedClient{statusActiveState: "inactive"}
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: t.TempDir(), Privileged: client, SystemdWantsDir: wants})
	state.inbounds = []Inbound{{Name: "sfhgs", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}
	ctx := NewManagementApplyContext(state)

	if _, _, _, err := ctx.promoteStagedConfigs(nil); err != nil {
		t.Fatalf("promote: %v", err)
	}
	if len(client.promotions) != 0 {
		t.Fatalf("no-op promote must not write artifacts: %+v", client.promotions)
	}
	if len(state.orphanedUnits) != 1 || state.orphanedUnits[0] != "veil-hysteria2@hy2-auto.service" {
		t.Fatalf("orphanedUnits = %v, want leftover hy2-auto", state.orphanedUnits)
	}

	ctx.reloadPromotedServices(nil)
	var got []string
	for _, action := range client.serviceActions {
		got = append(got, action.Unit+":"+string(action.Action))
	}
	want := []string{
		"veil-hysteria2@hy2-auto.service:stop",
		"veil-hysteria2@hy2-auto.service:disable",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("service actions = %v, want %v", got, want)
	}
}

func TestReloadPromotedServicesStopsOrphansAfterReloading(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: t.TempDir()})
	state.liveRoot = filepath.Join(state.applyRoot, "live")
	state.inbounds = []Inbound{{Name: "new", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}

	client := &recordingPrivilegedClient{statusActiveState: "inactive"}
	state.privileged = client
	state.privilegedLocal = false

	ctx := NewManagementApplyContext(state)
	state.orphanedUnits = []string{"veil-hysteria2@old.service"}
	liveFiles := []string{filepath.Join(state.liveRoot, "hysteria2", "new.yaml")}

	ctx.reloadPromotedServices(liveFiles)

	var got []string
	for _, a := range client.serviceActions {
		got = append(got, a.Unit+":"+string(a.Action))
	}
	want := []string{
		"veil-hysteria2@new.service:restart",
		"veil-hysteria2@new.service:enable",
		"veil-hysteria2@old.service:stop",
		"veil-hysteria2@old.service:disable",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("service actions = %v, want %v", got, want)
	}
	if len(state.orphanedUnits) != 0 {
		t.Fatalf("successfully cleaned orphan units must not be retried: %v", state.orphanedUnits)
	}
}
