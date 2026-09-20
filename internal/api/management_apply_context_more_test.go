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
}

func (f *recordingFirewallApplier) ApplySafely(rules []firewall.Rule) error {
	_ = rules
	return f.applySafelyErr
}

func boolPtr(v bool) *bool {
	return &v
}
