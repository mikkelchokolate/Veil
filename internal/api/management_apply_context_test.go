package api

import (
	"errors"
	"path/filepath"
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

type fakeApplyPrivileged struct {
	privilegedClient
	actions []privileged.ServiceActionRequest
}

func (f *fakeApplyPrivileged) ServiceAction(_ any, req privileged.ServiceActionRequest) error {
	f.actions = append(f.actions, req)
	return nil
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
	fake := &fakeApplyPrivileged{}
	state.privileged = fake
	state.privilegedLocal = false

	ctx := NewManagementApplyContext(state)
	results := ctx.reloadPromotedServicesLocked([]string{filepath.Join(state.liveRoot, "hysteria2", "edge.yaml")})
	if len(results) == 0 {
		t.Fatalf("expected service actions, got none")
	}
	if len(fake.actions) != 1 {
		t.Fatalf("actions = %+v", fake.actions)
	}
	if fake.actions[0].Unit != "veil-hysteria2@edge.service" || fake.actions[0].Action != privileged.ServiceActionRestart {
		t.Fatalf("unexpected action: %+v", fake.actions[0])
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
