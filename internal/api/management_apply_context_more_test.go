package api

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

func TestSyncCaddyCertForHysteria2(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newManagementState(ServerInfo{
		Mode:       "dev",
		Domain:     "vpn.example.com",
		Privileged: client,
	})
	ctx := NewManagementApplyContext(state)
	result := ctx.syncCaddyCertForHysteria2("vpn.example.com")
	if !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}

	stateNoPrivileged := newManagementState(ServerInfo{Mode: "dev", Domain: "vpn.example.com", RequirePrivilegedHelper: true})
	ctx = NewManagementApplyContext(stateNoPrivileged)
	result = ctx.syncCaddyCertForHysteria2("vpn.example.com")
	if result.Success || result.Error != "privileged helper is unavailable" {
		t.Fatalf("expected unavailable error, got %+v", result)
	}
}

func TestSyncFirewallLocked(t *testing.T) {
	state := newManagementState(ServerInfo{
		Mode:      "dev",
		ApplyRoot: t.TempDir(),
	})
	state.settings.FirewallManagement = boolPtr(true)
	state.inbounds = []Inbound{{Name: "h", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}

	old := firewallApplierInstance
	firewallApplierInstance = &recordingFirewallApplier{}
	t.Cleanup(func() { firewallApplierInstance = old })

	ctx := NewManagementApplyContext(state)
	results := ctx.syncFirewallLocked()
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("expected firewall success, got %+v", results)
	}

	firewallApplierInstance = &recordingFirewallApplier{ensureErr: errors.New("ufw not found")}
	results = ctx.syncFirewallLocked()
	if len(results) != 1 || results[0].Success {
		t.Fatalf("expected firewall failure, got %+v", results)
	}
}

func TestRunPrivilegedServiceActionUnavailable(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev", RequirePrivilegedHelper: true})
	ctx := NewManagementApplyContext(state)
	result := ctx.runPrivilegedServiceAction("veil.service", privileged.ServiceActionRestart)
	if result.Success || result.Error != "privileged helper is unavailable" {
		t.Fatalf("expected unavailable, got %+v", result)
	}
}

func TestWarpUnitActiveLocked(t *testing.T) {
	client := &recordingPrivilegedClient{statusActiveState: "active"}
	state := newManagementState(ServerInfo{Mode: "dev", Privileged: client})
	ctx := NewManagementApplyContext(state)
	if !ctx.warpUnitActiveLocked() {
		t.Fatal("expected WARP unit active")
	}

	stateNoPrivileged := newManagementState(ServerInfo{Mode: "dev"})
	ctx = NewManagementApplyContext(stateNoPrivileged)
	if ctx.warpUnitActiveLocked() {
		t.Fatal("expected WARP unit inactive without privileged helper")
	}
}

func TestHysteria2ConfigReloadNeeded(t *testing.T) {
	root := t.TempDir()
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: root, LiveRoot: filepath.Join(root, "live")})
	ctx := NewManagementApplyContext(state)
	liveFiles := []string{filepath.Join(root, "live", "hysteria2", "server.yaml")}
	if !ctx.hysteria2ConfigReloadNeeded(liveFiles) {
		t.Fatal("expected reload needed for hysteria2 live file")
	}
	if ctx.hysteria2ConfigReloadNeeded([]string{filepath.Join(root, "live", "caddy", "config.json")}) {
		t.Fatal("expected no reload for caddy file")
	}
}

type recordingFirewallApplier struct {
	ensureErr error
	applyErr  error
}

func (f *recordingFirewallApplier) EnsureActive() error { return f.ensureErr }
func (f *recordingFirewallApplier) ApplyRules(rules []firewall.Rule) error {
	_ = rules
	return f.applyErr
}

func boolPtr(v bool) *bool {
	return &v
}
