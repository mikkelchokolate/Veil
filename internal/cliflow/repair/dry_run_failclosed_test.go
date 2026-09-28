package repair

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
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

// TestBuildPlanWithoutYesDoesNotIssueLEIPCert is the #1128 regression: a bare
// `veil repair` (no --yes, no --dry-run) still ran ACME issuance during plan
// building — package installs, a remote script as root, the bound HTTP-01
// port, rewritten cert material — then ApplyPlan refused with "requires
// --yes" after the damage. Plan building must stay read-only until --yes.
func TestBuildPlanWithoutYesDoesNotIssueLEIPCert(t *testing.T) {
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
	// --yes gate issuance WOULD run — the assert is meaningful, not vacuous.
	if err := os.MkdirAll(filepath.Join(etcDir, "panel"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "panel", "tls.crt"), []byte("not-a-cert"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := BuildPlanFromOptions(Options{
		Profile: "ru-recommended", // neither Yes nor DryRun set
		EtcDir:  etcDir, VarDir: t.TempDir(), SystemdDir: t.TempDir(),
		LEIPCert: true, PublicIP: "203.0.113.10",
	}, PlanDependencies{Secret: func(label string) string { return "x-" + label }}); err != nil {
		t.Fatalf("BuildPlanFromOptions: %v", err)
	}
	if calls != 0 {
		t.Fatalf("plan building without --yes performed %d real ACME issuance(s)", calls)
	}
}

// TestBuildPlanWithoutYesDoesNotCommitState is the #1128 state.json half:
// plan building used to write settings.domain into state.json even without
// --yes. A bare `veil repair` must leave the store untouched.
func TestBuildPlanWithoutYesDoesNotCommitState(t *testing.T) {
	oldResolver := repairPublicIPResolver
	repairPublicIPResolver = func(ctx context.Context, value string, client *http.Client, endpoints []string) (net.IP, error) {
		return net.ParseIP("203.0.113.1"), nil
	}
	t.Cleanup(func() { repairPublicIPResolver = oldResolver })

	etcDir := t.TempDir()
	varDir := t.TempDir()
	systemdDir := t.TempDir()

	profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{PanelAccess: "direct", PanelPort: 25500, Secret: func(label string) string { return "direct-" + label }})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}
	if _, err := installer.ApplyRURecommendedProfile(profile, installer.ApplyPaths{EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir}); err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}

	key, err := secrets.LoadOrCreateKey(filepath.Join(etcDir, "state.key"))
	if err != nil {
		t.Fatalf("create state key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	statePath := filepath.Join(varDir, "state.json")
	before := managementstate.BuildSnapshot(managementstate.SnapshotInput{
		Settings: model.Settings{PanelListen: "0.0.0.0:25500", PanelAccess: "direct", Mode: "server"},
		Inbounds: []model.Inbound{},
		Rules:    []model.RoutingRule{},
		Warp:     model.WarpConfig{Endpoint: "engage.cloudflareclient.com:2408"},
	})
	if err := managementstate.NewStore(statePath, cipher).Save(before); err != nil {
		t.Fatalf("save state: %v", err)
	}
	beforeBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state before: %v", err)
	}

	if _, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir, PublicIP: "203.0.113.1"}); err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}

	afterBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state after: %v", err)
	}
	if !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatal("plan building without --yes mutated state.json (domain commit ran before the --yes gate)")
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
