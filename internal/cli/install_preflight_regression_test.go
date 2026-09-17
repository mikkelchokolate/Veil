package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/spf13/cobra"
)

// preflightEnv stubs every host seam collectInstallPreflight touches.
type preflightEnv struct {
	tools    map[string]bool
	users    map[string]bool
	files    map[string]bool
	manager  string
	root     bool
	systemd  bool
	foreign  string
	platform hostenv.Platform
}

func stubPreflightEnv(t *testing.T, env preflightEnv) {
	oldLookPath := execLookPath
	execLookPath = func(name string) (string, error) {
		if env.tools[name] {
			return "/usr/bin/" + name, nil
		}
		return "", fmt.Errorf("%s not found", name)
	}
	oldEuid := installGeteuidFunc
	installGeteuidFunc = func() int {
		if env.root {
			return 0
		}
		return 1000
	}
	oldManager := installPkgManagerFunc
	installPkgManagerFunc = func() string { return env.manager }
	oldUser := installUserLookupFunc
	installUserLookupFunc = func(name string) bool { return env.users[name] }
	oldSystemd := installSystemdPresentFunc
	installSystemdPresentFunc = func() bool { return env.systemd }
	oldFiles := installPathExistsFunc
	installPathExistsFunc = func(path string) bool { return env.files[path] }
	oldForeign := activeForeignFirewallFunc
	activeForeignFirewallFunc = func(context.Context) string { return env.foreign }
	oldPlatform := installPlatformFunc
	installPlatformFunc = func() hostenv.Platform {
		if env.platform.OS == "" {
			return hostenv.Platform{OS: "linux", Arch: "amd64"}
		}
		return env.platform
	}
	t.Cleanup(func() {
		execLookPath = oldLookPath
		installGeteuidFunc = oldEuid
		installPkgManagerFunc = oldManager
		installUserLookupFunc = oldUser
		installSystemdPresentFunc = oldSystemd
		installPathExistsFunc = oldFiles
		activeForeignFirewallFunc = oldForeign
		installPlatformFunc = oldPlatform
	})
}

func checkByID(report installPreflightReport, id string) (preflightCheck, bool) {
	for _, c := range report.Checks {
		if c.ID == id {
			return c, true
		}
	}
	return preflightCheck{}, false
}

func directProfileOpts() (installer.RURecommendedProfile, ruRecommendedInstallOptions) {
	opts := ruRecommendedInstallOptions{
		PanelAccess:  "direct",
		PanelPort:    3000,
		LEIPCert:     true,
		LEIPCertPort: 80,
		EtcDir:       "/etc/veil",
		VarDir:       "/var/lib/veil",
		SystemdDir:   defaultSystemdDir,
	}
	profile := installer.RURecommendedProfile{PanelAccess: "direct"}
	return profile, opts
}

// Audit #304: a host without systemd must be refused by the inventory before
// any download or state mutation, with an actionable remedy.
func TestPreflightRefusesNonSystemdHost(t *testing.T) {
	stubPreflightEnv(t, preflightEnv{
		root: true, systemd: false, manager: "apt-get",
		tools: map[string]bool{"systemctl": true, "sysctl": true, "ufw": true, "useradd": true,
			"curl": true, "openssl": true, "socat": true, "crontab": true,
			"tar": true, "gzip": true, "xz": true, "sha256sum": true},
		users: map[string]bool{"veil": true},
	})
	profile, opts := directProfileOpts()
	report := collectInstallPreflight(context.Background(), profile, opts)
	if report.ready() {
		t.Fatal("non-systemd host must not be install-ready")
	}
	c, ok := checkByID(report, "init")
	if !ok || c.Status != preflightMissing || !strings.Contains(c.Remedy, "systemd") {
		t.Fatalf("init check = %+v", c)
	}
}

func TestPreflightUnsupportedArchIsRejected(t *testing.T) {
	stubPreflightEnv(t, preflightEnv{
		root: true, systemd: true, manager: "apt-get",
		platform: hostenv.Platform{OS: "linux", Arch: "riscv64"},
		tools:    map[string]bool{},
		users:    map[string]bool{"veil": true},
	})
	profile, opts := directProfileOpts()
	report := collectInstallPreflight(context.Background(), profile, opts)
	c, _ := checkByID(report, "platform")
	if c.Status != preflightMissing || !strings.Contains(c.Remedy, "arm64") {
		t.Fatalf("platform check = %+v", c)
	}
	if report.ready() {
		t.Fatal("unsupported arch must not be install-ready")
	}
}

// Missing prerequisites that a package manager can supply are marked
// provision, and provisionInstallPreflight installs exactly those packages.
func TestPreflightProvisionsMissingPrereqs(t *testing.T) {
	stubPreflightEnv(t, preflightEnv{
		root: true, systemd: true, manager: "apt-get",
		tools: map[string]bool{
			"systemctl": true, "sysctl": true, "ufw": true, "useradd": true,
			"curl": true, "openssl": true, "tar": true, "gzip": true, "xz": true, "sha256sum": true,
			// socat and crontab absent
		},
		users: map[string]bool{"veil": true},
	})
	profile, opts := directProfileOpts()
	report := collectInstallPreflight(context.Background(), profile, opts)
	if !report.ready() {
		t.Fatalf("provisionable gaps must be install-ready: %s", report.String())
	}
	for _, id := range []string{"acme-socat", "acme-crontab"} {
		c, ok := checkByID(report, id)
		if !ok || c.Status != preflightProvision || c.Package == "" {
			t.Fatalf("%s = %+v, want provision with package", id, c)
		}
	}
	var calls [][]string
	oldCmd := provisionUFWCommandFunc
	provisionUFWCommandFunc = func(_ context.Context, name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	}
	t.Cleanup(func() { provisionUFWCommandFunc = oldCmd })
	if err := provisionInstallPreflight(context.Background(), report); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected one package-manager call, got %v", calls)
	}
	got := strings.Join(calls[0], " ")
	if !strings.HasPrefix(got, "apt-get install -y") ||
		!strings.Contains(got, "socat") || !strings.Contains(got, "cron") {
		t.Fatalf("unexpected provision command: %v", calls[0])
	}
}

func TestPreflightMissingWithoutManagerIsActionable(t *testing.T) {
	stubPreflightEnv(t, preflightEnv{
		root: true, systemd: true, manager: "",
		tools: map[string]bool{
			"systemctl": true, "sysctl": true, "ufw": true, "useradd": true,
			"curl": true, "openssl": true, "tar": true, "gzip": true, "xz": true, "sha256sum": true,
		},
		users: map[string]bool{"veil": true},
	})
	profile, opts := directProfileOpts()
	report := collectInstallPreflight(context.Background(), profile, opts)
	if report.ready() {
		t.Fatal("missing socat/crontab without a package manager must fail")
	}
	c, _ := checkByID(report, "acme-socat")
	if c.Status != preflightMissing || !strings.Contains(c.Remedy, "package manager") {
		t.Fatalf("acme-socat = %+v", c)
	}
}

func TestPreflightForeignFirewallRefused(t *testing.T) {
	stubPreflightEnv(t, preflightEnv{
		root: true, systemd: true, manager: "apt-get", foreign: "firewalld",
		tools: map[string]bool{
			"systemctl": true, "sysctl": true, "useradd": true,
			"curl": true, "openssl": true, "socat": true, "crontab": true,
			"tar": true, "gzip": true, "xz": true, "sha256sum": true,
		},
		users: map[string]bool{"veil": true},
	})
	profile, opts := directProfileOpts()
	report := collectInstallPreflight(context.Background(), profile, opts)
	c, _ := checkByID(report, "firewall")
	if c.Status != preflightMissing || !strings.Contains(c.Detail, "firewalld") {
		t.Fatalf("firewall = %+v", c)
	}
}

func TestPreflightSkipsHostChecksForNonRoot(t *testing.T) {
	stubPreflightEnv(t, preflightEnv{
		root: false, systemd: false, manager: "",
		tools: map[string]bool{
			"curl": true, "openssl": true, "socat": true, "crontab": true,
			"tar": true, "gzip": true, "xz": true, "sha256sum": true,
		},
	})
	profile, opts := directProfileOpts()
	report := collectInstallPreflight(context.Background(), profile, opts)
	for _, id := range []string{"init", "accounts", "firewall"} {
		c, ok := checkByID(report, id)
		if !ok || c.Status != preflightSkipped {
			t.Fatalf("%s = %+v, want skipped", id, c)
		}
	}
	if !report.ready() {
		t.Fatal("non-root report with all userland tools must be ready")
	}
}

func TestPreflightCaddyModeSkipsACME(t *testing.T) {
	stubPreflightEnv(t, preflightEnv{
		root: true, systemd: true, manager: "apt-get",
		tools: map[string]bool{
			"systemctl": true, "sysctl": true, "ufw": true, "useradd": true, "caddy": true,
			"tar": true, "gzip": true, "xz": true, "sha256sum": true,
		},
		users: map[string]bool{"veil": true},
	})
	profile := installer.RURecommendedProfile{PanelAccess: "caddy", InstallPanelCaddy: true}
	opts := ruRecommendedInstallOptions{PanelAccess: "caddy", LEIPCert: true, SystemdDir: defaultSystemdDir}
	report := collectInstallPreflight(context.Background(), profile, opts)
	c, ok := checkByID(report, "acme")
	if !ok || c.Status != preflightSkipped {
		t.Fatalf("acme = %+v", c)
	}
	cc, _ := checkByID(report, "caddy")
	if cc.Status != preflightOK {
		t.Fatalf("caddy = %+v", cc)
	}
}

// The --check flag prints the capability report and refuses unsupported hosts
// without touching state or runtimes.
func TestInstallCheckFlagPrintsReportAndRefuses(t *testing.T) {
	stubPreflightEnv(t, preflightEnv{
		root: true, systemd: false, manager: "",
		tools:    map[string]bool{"systemctl": true},
		platform: hostenv.Platform{OS: "linux", Arch: "amd64"},
	})
	oldRuntimes := installRuntimesFunc
	runtimesRan := false
	installRuntimesFunc = func(*cobra.Command, ruRecommendedInstallOptions) { runtimesRan = true }
	t.Cleanup(func() { installRuntimesFunc = oldRuntimes })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--check", "--panel-access", "direct"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("--check on a non-systemd host must fail")
	}
	output := out.String()
	if !strings.Contains(output, "capability report") || !strings.Contains(output, "init") {
		t.Fatalf("expected capability report, got:\n%s", output)
	}
	if runtimesRan {
		t.Fatal("--check must not reach the runtime phase")
	}
}
