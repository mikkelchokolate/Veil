package repair

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

var testExecutableFunc = func() (string, error) { return "", os.ErrNotExist }
var testLookPath = func(string) (string, error) { return "", os.ErrNotExist }

func buildRepairPlanFromOptions(opts Options) (installer.RepairPlan, error) {
	return BuildPlanFromOptions(opts, PlanDependencies{Secret: func(label string) string { return "repair-" + label }, Executable: testExecutableFunc, LookPath: testLookPath})
}

func TestBuildRepairPlanFromOptionsUsesCurrentExecutableForPanelUnit(t *testing.T) {
	oldExecutable := testExecutableFunc
	testExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	t.Cleanup(func() { testExecutableFunc = oldExecutable })

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: t.TempDir(), VarDir: t.TempDir(), SystemdDir: t.TempDir()})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	for _, action := range plan.Actions {
		if strings.HasSuffix(action.Path, "veil.service") && strings.Contains(action.Content, "ExecStart=/opt/veil/bin/veil serve") {
			return
		}
	}
	t.Fatalf("repair plan did not render veil.service with selected binary: %+v", plan.Actions)
}

func TestBuildRepairPlanFromOptionsPreservesExistingPanelSecrets(t *testing.T) {
	etcDir := t.TempDir()
	varDir := t.TempDir()
	systemdDir := t.TempDir()
	profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{Secret: func(label string) string { return "original-" + label }})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}
	if _, err := installer.ApplyRURecommendedProfile(profile, installer.ApplyPaths{EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir}); err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	oldExecutable := testExecutableFunc
	testExecutableFunc = func() (string, error) { return "/usr/local/bin/veil", nil }
	t.Cleanup(func() { testExecutableFunc = oldExecutable })

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	summary := plan.Summary()
	for _, unwanted := range []string{"veil.env", "panel/tls.crt", "panel/tls.key"} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("repair should preserve existing panel secret material, but planned %q:\n%s", unwanted, summary)
		}
	}
}

func TestBuildRepairPlanFromOptionsDoesNotReenablePanelTLSForCaddyAccess(t *testing.T) {
	etcDir := t.TempDir()
	varDir := t.TempDir()
	systemdDir := t.TempDir()
	directProfile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{PanelPort: 2096, Secret: func(label string) string { return "direct-" + label }})
	if err != nil {
		t.Fatalf("Build direct profile: %v", err)
	}
	if _, err := installer.ApplyRURecommendedProfile(directProfile, installer.ApplyPaths{EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir}); err != nil {
		t.Fatalf("Apply direct profile: %v", err)
	}
	caddyProfile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{PanelAccess: "caddy", Domain: "panel.example.com", Email: "admin@example.com", PanelPort: 2096, Secret: func(label string) string { return "caddy-" + label }})
	if err != nil {
		t.Fatalf("Build caddy profile: %v", err)
	}
	if _, err := installer.ApplyRURecommendedProfile(caddyProfile, installer.ApplyPaths{EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir}); err != nil {
		t.Fatalf("Apply caddy profile: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	for _, action := range plan.Actions {
		if strings.HasSuffix(action.Path, "veil.env") && strings.Contains(action.Content, "VEIL_TLS_CERT") {
			t.Fatalf("repair should not re-enable direct Panel TLS for caddy Panel access:\n%s", action.Content)
		}
	}
}

func TestBuildRepairPlanFromOptionsRepairsExistingPanelCaddyAccess(t *testing.T) {
	etcDir := t.TempDir()
	varDir := t.TempDir()
	systemdDir := t.TempDir()
	profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{PanelAccess: "caddy", Domain: "panel.example.com", Email: "admin@example.com", PanelPort: 2096, Secret: func(label string) string { return "original-" + label }})
	if err != nil {
		t.Fatalf("BuildRURecommendedProfile: %v", err)
	}
	if _, err := installer.ApplyRURecommendedProfile(profile, installer.ApplyPaths{EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir}); err != nil {
		t.Fatalf("ApplyRURecommendedProfile: %v", err)
	}
	if err := os.Remove(filepath.Join(etcDir, "generated", "caddy", "config.json")); err != nil {
		t.Fatalf("remove config.json: %v", err)
	}
	if err := os.Remove(filepath.Join(systemdDir, "veil-caddy.service")); err != nil {
		t.Fatalf("remove veil-caddy.service: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	summary := plan.Summary()
	for _, want := range []string{"generated/caddy/config.json", "veil-caddy.service"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("Panel Caddy repair summary missing %q:\n%s", want, summary)
		}
	}
}

func TestBuildRepairPlanFromOptionsBuildsPanelInstallPlan(t *testing.T) {
	plan, err := buildRepairPlanFromOptions(Options{
		Profile:    "ru-recommended",
		EtcDir:     t.TempDir(),
		VarDir:     t.TempDir(),
		SystemdDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	summary := plan.Summary()
	for _, want := range []string{"veil.service", "panel/tls.crt", "veil.env"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("repair summary missing %q:\n%s", want, summary)
		}
	}
	for _, want := range []string{"veil-helper.socket", "veil-hysteria2@.service", "veil-olcrtc@.service", "veil-mieru.service"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("repair summary missing managed unit template %q:\n%s", want, summary)
		}
	}
	for _, unwanted := range []string{"generated/caddy/Caddyfile", "generated/hysteria2", "generated/mieru"} {
		if strings.Contains(summary, unwanted) {
			t.Fatalf("Panel install repair should not include %q:\n%s", unwanted, summary)
		}
	}
}

func TestBuildRepairPlanFromOptionsRegeneratesDirectPanelTLSCertWithInterfaceIPs(t *testing.T) {
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
	if err := os.Remove(filepath.Join(etcDir, "panel", "tls.crt")); err != nil {
		t.Fatalf("remove tls.crt: %v", err)
	}
	if err := os.Remove(filepath.Join(etcDir, "panel", "tls.key")); err != nil {
		t.Fatalf("remove tls.key: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}

	var certPEM string
	for _, action := range plan.Actions {
		if strings.HasSuffix(action.Path, "panel/tls.crt") {
			certPEM = action.Content
			break
		}
	}
	if certPEM == "" {
		t.Fatal("repair plan did not include regenerated panel/tls.crt")
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("failed to decode regenerated certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	hasLoopback := false
	for _, ip := range cert.IPAddresses {
		if ip.IsLoopback() {
			hasLoopback = true
			break
		}
	}
	if !hasLoopback {
		t.Fatalf("expected regenerated cert to include loopback IP, got %v", cert.IPAddresses)
	}

	// The regenerated direct-mode cert must include at least one non-loopback
	// interface IP so browsers can validate it against the public IP.
	var extraIPs []net.IP
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && !ip.IsLoopback() {
				extraIPs = append(extraIPs, ip)
			}
		}
	}
	if len(extraIPs) == 0 {
		t.Skip("no non-loopback interfaces available in test environment")
	}
	for _, want := range extraIPs {
		found := false
		for _, got := range cert.IPAddresses {
			if got.Equal(want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected regenerated cert to include interface IP %v, got %v", want, cert.IPAddresses)
		}
	}
}

func TestBuildRepairPlanFromOptionsIssuesLEIPCertForDirectMode(t *testing.T) {
	oldIssue := leIPCertIssueFunc
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		if err := os.WriteFile(opts.CertPath, []byte("LE-CERT-PEM"), 0o644); err != nil {
			t.Fatalf("write fake cert: %v", err)
		}
		if err := os.WriteFile(opts.KeyPath, []byte("LE-KEY-PEM"), 0o640); err != nil {
			t.Fatalf("write fake key: %v", err)
		}
		return acmeip.IssuedCert{CertPath: opts.CertPath, KeyPath: opts.KeyPath}, nil
	}
	t.Cleanup(func() { leIPCertIssueFunc = oldIssue })

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
	// Force renewal by replacing the certificate with invalid PEM.
	if err := os.WriteFile(filepath.Join(etcDir, "panel", "tls.crt"), []byte("not-a-cert"), 0o644); err != nil {
		t.Fatalf("write invalid cert: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir, LEIPCert: true, LEIPCertPort: 80, PublicIP: "127.0.0.1"})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	_ = plan

	// acme.sh (and our fake) writes the certificate directly to disk, so the
	// repair plan may not need a drift action, but the on-disk file must be
	// the issued certificate.
	certContent, err := os.ReadFile(filepath.Join(etcDir, "panel", "tls.crt"))
	if err != nil {
		t.Fatalf("read cert after repair: %v", err)
	}
	if string(certContent) != "LE-CERT-PEM" {
		t.Fatalf("expected on-disk cert to be issued LE cert, got %q", certContent)
	}

	// The veil.env action (or existing file) must reference the cert path.
	envContent, err := os.ReadFile(filepath.Join(etcDir, "veil.env"))
	if err != nil {
		t.Fatalf("read veil.env after repair: %v", err)
	}
	if !strings.Contains(string(envContent), "VEIL_TLS_CERT=") {
		t.Fatalf("veil.env missing VEIL_TLS_CERT reference:\n%s", envContent)
	}
}

func TestBuildRepairPlanFromOptionsFillsDomainForDirectMode(t *testing.T) {
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
	snapshot := managementstate.BuildSnapshot(managementstate.SnapshotInput{
		Settings: model.Settings{PanelListen: "0.0.0.0:25500", PanelAccess: "direct", Mode: "server"},
		Inbounds: []model.Inbound{},
		Rules:    []model.RoutingRule{},
		Warp:     model.WarpConfig{Endpoint: "engage.cloudflareclient.com:2408"},
	})
	if err := managementstate.NewStore(statePath, cipher).Save(snapshot); err != nil {
		t.Fatalf("save state: %v", err)
	}

	plan, err := buildRepairPlanFromOptions(Options{Profile: "ru-recommended", EtcDir: etcDir, VarDir: varDir, SystemdDir: systemdDir, PublicIP: "203.0.113.1"})
	if err != nil {
		t.Fatalf("buildRepairPlanFromOptions: %v", err)
	}
	_ = plan

	envContent := repairActionContent(plan, "veil.env")
	if !strings.Contains(envContent, "VEIL_DOMAIN=203.0.113.1") {
		t.Fatalf("veil.env action should contain resolved public IP as domain, got:\n%s", envContent)
	}

	updated, ok, err := managementstate.NewStore(statePath, cipher).Load()
	if err != nil {
		t.Fatalf("load updated state: %v", err)
	}
	if !ok {
		t.Fatal("updated state missing")
	}
	if updated.Settings.Domain != "203.0.113.1" {
		t.Fatalf("expected state domain updated to public IP, got %q", updated.Settings.Domain)
	}
}
