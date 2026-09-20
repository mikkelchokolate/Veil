package repair

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/installer"
)

// TestRepairLEIPCertHonoursControlledCA is the #338 regression: repair-time
// Let's Encrypt IP certificate renewal rebuilt IssueOptions without the
// controlled-CA knobs, so a renewal could silently leave the operator's CA.
// The persisted VEIL_ACME_CA_URL (or an explicit env override) must reach
// issuance together with VEIL_ACME_INSECURE.
func TestRepairLEIPCertHonoursControlledCA(t *testing.T) {
	var got acmeip.IssueOptions
	oldIssue := leIPCertIssueFunc
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		got = opts
		return acmeip.IssuedCert{CertPath: opts.CertPath, KeyPath: opts.KeyPath}, nil
	}
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })

	oldResolver := repairLEPublicIPResolver
	repairLEPublicIPResolver = func(ctx context.Context, value string, client *http.Client, endpoints []string) (net.IP, error) {
		return net.ParseIP("203.0.113.10"), nil
	}
	t.Cleanup(func() { repairLEPublicIPResolver = oldResolver })

	t.Setenv("VEIL_ACME_INSECURE", "1")
	t.Setenv("VEIL_ACME_CA_URL", "")
	t.Setenv("VEIL_ACME_CA_ROOT", "")

	etcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte("VEIL_PANEL_ACCESS=direct\nVEIL_LISTEN=0.0.0.0:25500\nVEIL_ACME_CA_URL=https://ca.internal/acme\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(etcDir, "panel"), 0o700); err != nil {
		t.Fatalf("mkdir panel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "panel", "tls.crt"), []byte("not-a-cert"), 0o644); err != nil {
		t.Fatalf("write invalid cert: %v", err)
	}
	// maybeIssueLEIPCert reads the issued PEM back into the profile.
	if err := os.WriteFile(filepath.Join(etcDir, "panel", "tls.key"), []byte("key"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	if _, err := BuildPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: t.TempDir(), SystemdDir: t.TempDir(), LEIPCert: true, PublicIP: "203.0.113.10"}, PlanDependencies{Secret: func(label string) string { return "x-" + label }}); err != nil {
		t.Fatalf("BuildPlanFromOptions: %v", err)
	}
	if got.CAServer != "https://ca.internal/acme" {
		t.Fatalf("IssueOptions.CAServer = %q, want the persisted controlled CA", got.CAServer)
	}
	if !got.Insecure {
		t.Fatal("IssueOptions.Insecure must follow VEIL_ACME_INSECURE")
	}
}

// TestRepairPreservesACMEEnv is the #340 regression: repair rewrites veil.env
// and used to drop the controlled-CA variables install persisted, moving the
// running panel's Caddy issuers back to Let's Encrypt. Both the profile-based
// rewrite and the settings-snapshot rewrite must keep them.
func TestRepairPreservesACMEEnv(t *testing.T) {
	t.Setenv("VEIL_ACME_CA_URL", "")
	t.Setenv("VEIL_ACME_CA_ROOT", "")

	etcDir := t.TempDir()
	envBody := "VEIL_API_TOKEN=tok-1\nVEIL_PANEL_ACCESS=local\nVEIL_LISTEN=127.0.0.1:2096\nVEIL_ACME_CA_URL=https://ca.internal/acme\nVEIL_ACME_CA_ROOT=/etc/veil/ca.pem\n"
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte(envBody), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}

	plan, err := BuildPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: t.TempDir(), SystemdDir: t.TempDir()}, PlanDependencies{Secret: func(label string) string { return "x-" + label }})
	if err != nil {
		t.Fatalf("BuildPlanFromOptions: %v", err)
	}
	env := repairPlanEnvContent(plan, filepath.Join(etcDir, "veil.env"))
	if env == "" {
		t.Fatalf("plan has no veil.env action: %+v", plan)
	}
	if !strings.Contains(env, "VEIL_ACME_CA_URL=https://ca.internal/acme") {
		t.Fatalf("rewritten veil.env lost VEIL_ACME_CA_URL:\n%s", env)
	}
	if !strings.Contains(env, "VEIL_ACME_CA_ROOT=/etc/veil/ca.pem") {
		t.Fatalf("rewritten veil.env lost VEIL_ACME_CA_ROOT:\n%s", env)
	}
}

// TestRepairACMEEnvPrefersExplicitOverride pins the precedence: an operator
// re-running repair with VEIL_ACME_CA_URL set updates the persisted config
// rather than silently keeping the stale value.
func TestRepairACMEEnvPrefersExplicitOverride(t *testing.T) {
	etcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte("VEIL_ACME_CA_URL=https://old.internal/acme\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	t.Setenv("VEIL_ACME_CA_URL", "https://new.internal/acme")
	t.Setenv("VEIL_ACME_CA_ROOT", "")
	caURL, _ := repairACMEEnv(etcDir)
	if caURL != "https://new.internal/acme" {
		t.Fatalf("repairACMEEnv = %q, want the explicit override", caURL)
	}
}

func repairPlanEnvContent(plan installer.RepairPlan, path string) string {
	for _, action := range plan.Actions {
		if action.Path == path {
			return action.Content
		}
	}
	return ""
}
