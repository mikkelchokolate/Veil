package repair

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/installer"
)

// TestBuildPlanDryRunDoesNotIssueLEIPCert is the #1021 regression: plan
// building runs under `veil repair --dry-run`, so ACME issuance inside it —
// package installs, curl|sh, binding the HTTP-01 port, writing cert material —
// is real mutation that must be skipped until apply.
func TestBuildPlanDryRunDoesNotIssueLEIPCert(t *testing.T) {
	calls := 0
	oldIssue := leIPCertIssueFunc
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		calls++
		return acmeip.IssuedCert{CertPath: opts.CertPath, KeyPath: opts.KeyPath}, nil
	}
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })

	etcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte("VEIL_PANEL_ACCESS=direct\nVEIL_LISTEN=0.0.0.0:25500\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	// A stale/invalid cert forces shouldRenewLEIPCert=true, so without the
	// dry-run gate issuance WOULD run — the assert is meaningful, not vacuous.
	if err := os.MkdirAll(filepath.Join(etcDir, "panel"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "panel", "tls.crt"), []byte("not-a-cert"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := BuildPlanFromOptions(Options{
		Profile: "ru-recommended", DryRun: true,
		EtcDir: etcDir, VarDir: t.TempDir(), SystemdDir: t.TempDir(),
		LEIPCert: true, PublicIP: "203.0.113.10",
	}, PlanDependencies{Secret: func(label string) string { return "x-" + label }}); err != nil {
		t.Fatalf("BuildPlanFromOptions: %v", err)
	}
	if calls != 0 {
		t.Fatalf("dry-run performed %d real ACME issuance(s)", calls)
	}
}

// TestBuildPlanFailsClosedWhenSecretReturnsEmpty is the #1022 repair-side
// regression: an empty token is the generator's crypto/rand failure signal —
// the plan must error, not install a known credential.
func TestBuildPlanFailsClosedWhenSecretReturnsEmpty(t *testing.T) {
	_, err := BuildPlanFromOptions(Options{
		Profile: "ru-recommended", EtcDir: t.TempDir(), VarDir: t.TempDir(), SystemdDir: t.TempDir(),
	}, PlanDependencies{Secret: func(label string) string { return "" }})
	if err == nil {
		t.Fatal("BuildPlanFromOptions must fail closed when the secret generator returns empty")
	}
}

// TestRepairPanelAuthTokenPropagatesGeneratorFailure pins the fail-closed
// empty-token contract on the repair path (issue #1022).
func TestRepairPanelAuthTokenPropagatesGeneratorFailure(t *testing.T) {
	if _, err := repairPanelAuthToken(installer.RepairPlan{}, t.TempDir(), func(label string) string { return "" }); err == nil {
		t.Fatal("repairPanelAuthToken must reject an empty generated token")
	}
}
