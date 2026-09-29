package api

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

func TestSyncCaddyCertForHysteria2(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newManagementState(ServerInfo{
		Mode:       "dev",
		Domain:     "vpn.example.com",
		Privileged: client,
	})
	ctx := NewManagementApplyContext(state)
	result, pending := ctx.syncCaddyCertForHysteria2("vpn.example.com")
	if !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if pending {
		t.Fatal("a found certificate must not be marked pending")
	}

	stateNoPrivileged := newManagementState(ServerInfo{Mode: "dev", Domain: "vpn.example.com", RequirePrivilegedHelper: true})
	ctx = NewManagementApplyContext(stateNoPrivileged)
	result, pending = ctx.syncCaddyCertForHysteria2("vpn.example.com")
	if result.Success || result.Error != "privileged helper is unavailable" {
		t.Fatalf("expected unavailable error, got %+v", result)
	}
	if pending {
		t.Fatal("a helper failure must not be marked pending")
	}
}

func TestSyncFirewallLocked(t *testing.T) {
	state := newManagementState(ServerInfo{
		Mode:      "dev",
		ApplyRoot: t.TempDir(),
	})
	state.settings.FirewallManagement = boolPtr(true)
	state.inbounds = []Inbound{{Name: "h", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}

	old := currentFirewallApplier()
	swapFirewallApplier(&recordingFirewallApplier{})
	t.Cleanup(func() { swapFirewallApplier(old) })

	ctx := NewManagementApplyContext(state)
	results := ctx.syncFirewall()
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("expected firewall success, got %+v", results)
	}

	swapFirewallApplier(&recordingFirewallApplier{applySafelyErr: errors.New("ufw not found")})
	results = ctx.syncFirewall()
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

// TestUnitTeardownPending locks #1133: teardown decisions must consider unit
// presence — running, enabled-but-inactive, merely loaded, or status-errored
// — and observable artifacts, not just ActiveState == "active".
func TestUnitTeardownPending(t *testing.T) {
	root := t.TempDir()
	newState := func(client *recordingPrivilegedClient) *managementState {
		return newManagementState(ServerInfo{Mode: "dev", ApplyRoot: root, Privileged: client})
	}
	warpPending := func(state *managementState) bool {
		return NewManagementApplyContext(state).unitTeardownPending(
			renderer.UnitWarp, generatedconfig.WarpConfigSubpath)
	}

	if !warpPending(newState(&recordingPrivilegedClient{statusActiveState: "active"})) {
		t.Fatal("active WARP unit must be torn down")
	}
	// Enabled but not running — the stale config still needs to go and the
	// enablement must be removed or WARP returns after a reboot.
	if !warpPending(newState(&recordingPrivilegedClient{
		statusActiveState: "inactive", statusUnitFileState: "enabled",
	})) {
		t.Fatal("enabled-but-inactive WARP unit must be torn down")
	}
	// A status query that cannot answer must not leave stale configuration:
	// errored presence is treated as present (fail toward retiring).
	if !warpPending(newState(&recordingPrivilegedClient{statusError: "unit may be dead"})) {
		t.Fatal("status-errored WARP unit must be torn down")
	}
	// Loaded but disabled and inactive with no artifact left —
	// nothing pending.
	state := newState(&recordingPrivilegedClient{
		statusActiveState: "inactive", statusUnitFileState: "disabled",
	})
	if warpPending(state) {
		t.Fatal("inert WARP unit with no artifact must not be torn down")
	}
	// A live artifact on disk is evidence even when the unit is gone.
	artifact := filepath.Join(root, "live", filepath.FromSlash(generatedconfig.WarpConfigSubpath))
	if err := os.MkdirAll(filepath.Dir(artifact), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !warpPending(state) {
		t.Fatal("stale live warp.json must trigger teardown")
	}
}

func TestHysteria2CertDomainsIgnoresMissingAndNonHysteria2Files(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "live", "hysteria2", "h.yaml")
	caddy := filepath.Join(root, "live", "caddy", "config.json")
	if domains := hysteria2CertDomainsFromConfigs([]string{missing, caddy}); len(domains) != 0 {
		t.Fatalf("expected no sync domains for missing/caddy files, got %v", domains)
	}
}

type recordingFirewallApplier struct {
	applySafelyErr error
	pruneCalls     int
	pruneGotRules  []firewall.Rule
	pruneResult    int
	pruneErr       error
}

func (f *recordingFirewallApplier) ApplySafely(rules []firewall.Rule) error {
	_ = rules
	return f.applySafelyErr
}

func (f *recordingFirewallApplier) PruneStaleManagedRules(desired []firewall.Rule) (int, error) {
	f.pruneCalls++
	f.pruneGotRules = desired
	return f.pruneResult, f.pruneErr
}

func boolPtr(v bool) *bool {
	return &v
}

// TestPrepareFirewallLockedReconcilesEmptyDesired is the #356 follow-up: a
// local/loopback panel produces zero desired UFW rules, but the privileged
// reconcile must still run so stale Veil-managed rules (for example a panel
// allow left behind by a public -> local switch) are pruned instead of
// stranded in ufw forever. It also keeps the apply honest: an unavailable
// helper still fails the apply instead of silently skipping the boundary.
// #538: the local/dev apply path mutates UFW directly via ApplySafely — no
// privileged staged transaction exists. PrepareFirewallLocked records the
// mutation with the "local" sentinel so the workflow counts the firewall as
// changed, and RollbackFirewallLocked must refuse to claim an undo that
// never ran (which would have reported FirewallRestored vacuously).
func TestLocalFirewallSyncSentinelCannotBeRolledBack(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev", ApplyRoot: t.TempDir()})
	state.settings.FirewallManagement = boolPtr(true)
	state.inbounds = []Inbound{{Name: "h", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true}}

	old := currentFirewallApplier()
	swapFirewallApplier(&recordingFirewallApplier{})
	t.Cleanup(func() { swapFirewallApplier(old) })

	ctx := NewManagementApplyContext(state)
	txID, err := ctx.PrepareFirewallLocked()
	if err != nil {
		t.Fatalf("PrepareFirewallLocked: %v", err)
	}
	if txID != localFirewallSyncTransactionID {
		t.Fatalf("local sync transaction = %q, want %q", txID, localFirewallSyncTransactionID)
	}
	if err := ctx.RollbackFirewallLocked(txID); err == nil {
		t.Fatal("rollback of a non-transactional local sync must not report success")
	}
	if err := ctx.RollbackFirewallLocked(""); err != nil {
		t.Fatalf("empty transaction must remain a no-op: %v", err)
	}
}

func TestPrepareFirewallLockedReconcilesEmptyDesired(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev"})
	state.settings.PanelAccess = "local"
	state.settings.PanelListen = "127.0.0.1:2096"
	client := &recordingPrivilegedClient{
		firewallResult: privileged.FirewallResult{Prepared: true, TransactionID: "tx-1"},
	}
	state.privileged = client
	state.privilegedLocal = false

	ctx := NewManagementApplyContext(state)
	transactionID, err := ctx.PrepareFirewallLocked()
	if err != nil {
		t.Fatalf("PrepareFirewallLocked: %v", err)
	}
	if transactionID != "tx-1" {
		t.Fatalf("transaction id = %q, want tx-1", transactionID)
	}
	if len(client.firewallRequests) != 1 {
		t.Fatalf("expected one firewall prepare, got %+v", client.firewallRequests)
	}
	request := client.firewallRequests[0]
	if request.Action != privileged.FirewallActionPrepare {
		t.Fatalf("action = %q, want prepare", request.Action)
	}
	if len(request.Rules) != 0 {
		t.Fatalf("local panel must produce an empty desired set, got %+v", request.Rules)
	}
}
