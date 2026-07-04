package repair

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/api"
	"github.com/mikkelchokolate/Veil/internal/installer"
)

func TestBuildPlanFromOptionsReturnsErrorWhenBuildRepairPlanFails(t *testing.T) {
	_, err := BuildPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: "", VarDir: "", SystemdDir: t.TempDir()}, PlanDependencies{Secret: func(label string) string { return "x-" + label }})
	if err == nil {
		t.Fatal("expected BuildRepairPlan error")
	}
}

func TestBuildPlanFromOptionsPropagatesAddPanelStateRepairActionsError(t *testing.T) {
	etcDir := t.TempDir()
	varDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(varDir, "state.json"), []byte("not valid json"), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	_, err := BuildPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: t.TempDir()}, PlanDependencies{Secret: func(label string) string { return "x-" + label }})
	if err == nil {
		t.Fatal("expected addPanelStateRepairActions error")
	}
}

func TestBuildPlanFromOptionsHandlesPublicIPResolverError(t *testing.T) {
	oldResolver := repairPublicIPResolver
	repairPublicIPResolver = func(ctx context.Context, value string, client *http.Client, endpoints []string) (net.IP, error) {
		return nil, errors.New("no IP")
	}
	t.Cleanup(func() { repairPublicIPResolver = oldResolver })

	etcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte("VEIL_PANEL_ACCESS=direct\nVEIL_LISTEN=0.0.0.0:25500\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}

	_, err := BuildPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: t.TempDir(), SystemdDir: t.TempDir()}, PlanDependencies{Secret: func(label string) string { return "x-" + label }})
	if err != nil {
		t.Fatalf("expected resolver error to be swallowed, got %v", err)
	}
}

func TestBuildPlanFromOptionsSwallowsLEIPCertError(t *testing.T) {
	oldIssue := leIPCertIssueFunc
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		return acmeip.IssuedCert{}, errors.New("LE issue failed")
	}
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })

	etcDir := t.TempDir()
	varDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte("VEIL_PANEL_ACCESS=direct\nVEIL_LISTEN=0.0.0.0:25500\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(etcDir, "panel"), 0o700); err != nil {
		t.Fatalf("mkdir panel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "panel", "tls.crt"), []byte("not-a-cert"), 0o644); err != nil {
		t.Fatalf("write invalid cert: %v", err)
	}

	_, err := BuildPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: t.TempDir(), LEIPCert: true, PublicIP: "127.0.0.1"}, PlanDependencies{Secret: func(label string) string { return "x-" + label }})
	if err != nil {
		t.Fatalf("expected LE error to be swallowed, got %v", err)
	}
}

func TestSecretDefaultReturnsLabel(t *testing.T) {
	got := PlanDependencies{}.secret()("panel")
	if got != "panel" {
		t.Fatalf("expected default secret to return label, got %q", got)
	}
}

func TestExecutableDefaultReturnsExecutable(t *testing.T) {
	exe, err := PlanDependencies{}.executable()()
	if err != nil {
		t.Fatalf("default executable: %v", err)
	}
	if exe == "" {
		t.Fatal("default executable returned empty path")
	}
}

func TestLookPathDefaultReturnsLookPath(t *testing.T) {
	path, err := PlanDependencies{}.lookPath()("go")
	if err != nil {
		t.Fatalf("default lookPath: %v", err)
	}
	if path == "" {
		t.Fatal("default lookPath returned empty path")
	}
}

func TestPreserveExistingPanelRepairMaterialNoops(t *testing.T) {
	profile := installer.RURecommendedProfile{PanelAuthToken: "keep"}
	preserveExistingPanelRepairMaterial(nil, t.TempDir())
	preserveExistingPanelRepairMaterial(&profile, "")
	if profile.PanelAuthToken != "keep" {
		t.Fatal("profile should be unchanged")
	}
}

func TestPanelPortFromListen(t *testing.T) {
	cases := []struct {
		listen string
		want   int
	}{
		{"127.0.0.1", 0},
		{"127.0.0.1:abc", 0},
		{"127.0.0.1:80:443", 443},
		{"[::1]:2096", 2096},
		{"[::1]", 0},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.want)+"_"+tc.listen, func(t *testing.T) {
			if got := panelPortFromListen(tc.listen); got != tc.want {
				t.Fatalf("panelPortFromListen(%q) = %d, want %d", tc.listen, got, tc.want)
			}
		})
	}
}

func TestReadRepairEnvSkipsInvalidLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "veil.env")
	body := "# comment\n\nKEY=value\nNOEQUAL\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	values := readRepairEnv(path)
	if values["KEY"] != "value" {
		t.Fatalf("expected KEY=value, got %v", values)
	}
	if _, ok := values["NOEQUAL"]; ok {
		t.Fatal("expected NOEQUAL to be skipped")
	}
}

func TestRepairStateCipher(t *testing.T) {
	if cipher := repairStateCipher(filepath.Join(t.TempDir(), "missing.key")); cipher != nil {
		t.Fatal("expected nil cipher for missing key")
	}
	keyPath := filepath.Join(t.TempDir(), "bad.key")
	if err := os.WriteFile(keyPath, []byte("short"), 0o600); err != nil {
		t.Fatalf("write bad key: %v", err)
	}
	if cipher := repairStateCipher(keyPath); cipher != nil {
		t.Fatal("expected nil cipher for invalid key")
	}
}

func TestAddRepairFileActionReturnsReadError(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "blocked"), []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	plan := &installer.RepairPlan{}
	err := addRepairFileAction(plan, filepath.Join(tmp, "blocked", "x"), "content", 0o600)
	if err == nil {
		t.Fatal("expected read error")
	}
}

func TestSetRepairFileActionUpdatesExisting(t *testing.T) {
	plan := &installer.RepairPlan{Actions: []installer.RepairAction{{Path: "/etc/veil/veil.env", Content: "old", Mode: 0o600}}}
	if err := setRepairFileAction(plan, "/etc/veil/veil.env", "new", 0o644); err != nil {
		t.Fatalf("setRepairFileAction: %v", err)
	}
	if plan.Actions[0].Content != "new" || plan.Actions[0].Mode != 0o644 {
		t.Fatalf("action not updated: %+v", plan.Actions[0])
	}
}

func TestApplyPanelSettingsRepairActionsNoPanelAccess(t *testing.T) {
	plan := installer.RepairPlan{}
	if err := applyPanelSettingsRepairActions(&plan, Options{EtcDir: t.TempDir()}, api.Settings{}, func(label string) string { return label }); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestShouldRenewLEIPCert(t *testing.T) {
	cases := []struct {
		name     string
		certPath string
		issuer   string
		notAfter time.Time
		want     bool
	}{
		{"missing", "", "Let's Encrypt", time.Now().Add(30 * 24 * time.Hour), true},
		{"invalid PEM", "invalid.pem", "Let's Encrypt", time.Now().Add(30 * 24 * time.Hour), true},
		{"LE fresh", "le-fresh.pem", "Let's Encrypt", time.Now().Add(30 * 24 * time.Hour), false},
		{"LE expired soon", "le-soon.pem", "Let's Encrypt", time.Now().Add(1 * 24 * time.Hour), true},
		{"not LE", "not-le.pem", "Veil", time.Now().Add(30 * 24 * time.Hour), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var certPath string
			if tc.certPath != "" {
				certPath = filepath.Join(t.TempDir(), tc.certPath)
				if tc.name == "invalid PEM" {
					if err := os.WriteFile(certPath, []byte("not a valid PEM file"), 0o644); err != nil {
						t.Fatalf("write invalid pem: %v", err)
					}
				} else {
					writeTestCert(t, certPath, tc.issuer, tc.notAfter)
				}
			}
			if got := shouldRenewLEIPCert(certPath); got != tc.want {
				t.Fatalf("shouldRenewLEIPCert(%q) = %v, want %v", certPath, got, tc.want)
			}
		})
	}
}

func writeTestCert(t *testing.T, path, issuer string, notAfter time.Time) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{Organization: []string{issuer}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"example.com"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
}

func TestMaybeIssueLEIPCertSkipsWhenNotNeeded(t *testing.T) {
	etcDir := t.TempDir()
	certPath := filepath.Join(etcDir, "panel", "tls.crt")
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeTestCert(t, certPath, "Let's Encrypt", time.Now().Add(30*24*time.Hour))

	oldIssue := leIPCertIssueFunc
	called := false
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		called = true
		return acmeip.IssuedCert{}, nil
	}
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })

	profile := installer.RURecommendedProfile{Email: "admin@example.com"}
	if err := maybeIssueLEIPCert(context.Background(), &profile, Options{EtcDir: etcDir, PublicIP: "127.0.0.1"}); err != nil {
		t.Fatalf("maybeIssueLEIPCert: %v", err)
	}
	if called {
		t.Fatal("LE issue should not be called when cert is fresh")
	}
}

func TestMaybeIssueLEIPCertReturnsErrorOnInvalidPublicIP(t *testing.T) {
	etcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(etcDir, "panel"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "panel", "tls.crt"), []byte("bad"), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	profile := installer.RURecommendedProfile{Email: "admin@example.com"}
	err := maybeIssueLEIPCert(context.Background(), &profile, Options{EtcDir: etcDir, PublicIP: "not-an-ip"})
	if err == nil || !strings.Contains(err.Error(), "detect public IP") {
		t.Fatalf("expected detect public IP error, got %v", err)
	}
}

func TestMaybeIssueLEIPCertReturnsErrorOnNilIP(t *testing.T) {
	etcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(etcDir, "panel"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "panel", "tls.crt"), []byte("bad"), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	oldResolver := repairLEPublicIPResolver
	repairLEPublicIPResolver = func(ctx context.Context, value string, client *http.Client, endpoints []string) (net.IP, error) {
		return nil, nil
	}
	t.Cleanup(func() { repairLEPublicIPResolver = oldResolver })

	profile := installer.RURecommendedProfile{Email: "admin@example.com"}
	err := maybeIssueLEIPCert(context.Background(), &profile, Options{EtcDir: etcDir, PublicIP: "auto"})
	if err == nil || !strings.Contains(err.Error(), "public IP detection returned empty") {
		t.Fatalf("expected empty public IP error, got %v", err)
	}
}

func TestMaybeIssueLEIPCertReturnsErrorOnIssueFailure(t *testing.T) {
	etcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(etcDir, "panel"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "panel", "tls.crt"), []byte("bad"), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	oldIssue := leIPCertIssueFunc
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		return acmeip.IssuedCert{}, errors.New("issue failed")
	}
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })

	profile := installer.RURecommendedProfile{Email: "admin@example.com"}
	err := maybeIssueLEIPCert(context.Background(), &profile, Options{EtcDir: etcDir, PublicIP: "127.0.0.1"})
	if err == nil || !strings.Contains(err.Error(), "issue failed") {
		t.Fatalf("expected issue failure error, got %v", err)
	}
}

func TestMaybeIssueLEIPCertReturnsErrorOnCertRead(t *testing.T) {
	etcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(etcDir, "panel"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "panel", "tls.crt"), []byte("bad"), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	oldIssue := leIPCertIssueFunc
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		return acmeip.IssuedCert{CertPath: filepath.Join(etcDir, "missing.crt"), KeyPath: filepath.Join(etcDir, "missing.key")}, nil
	}
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })

	profile := installer.RURecommendedProfile{Email: "admin@example.com"}
	err := maybeIssueLEIPCert(context.Background(), &profile, Options{EtcDir: etcDir, PublicIP: "127.0.0.1"})
	if err == nil || !strings.Contains(err.Error(), "read issued certificate") {
		t.Fatalf("expected cert read error, got %v", err)
	}
}

func TestMaybeIssueLEIPCertReturnsErrorOnKeyRead(t *testing.T) {
	etcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(etcDir, "panel"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "panel", "tls.crt"), []byte("bad"), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	certPath := filepath.Join(etcDir, "issued.crt")
	writeTestCert(t, certPath, "Let's Encrypt", time.Now().Add(30*24*time.Hour))

	oldIssue := leIPCertIssueFunc
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		return acmeip.IssuedCert{CertPath: certPath, KeyPath: filepath.Join(etcDir, "missing.key")}, nil
	}
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })

	profile := installer.RURecommendedProfile{Email: "admin@example.com"}
	err := maybeIssueLEIPCert(context.Background(), &profile, Options{EtcDir: etcDir, PublicIP: "127.0.0.1"})
	if err == nil || !strings.Contains(err.Error(), "read issued key") {
		t.Fatalf("expected key read error, got %v", err)
	}
}
