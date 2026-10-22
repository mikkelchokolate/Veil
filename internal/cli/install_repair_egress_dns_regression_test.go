package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	doctorflow "github.com/mikkelchokolate/Veil/internal/cliflow/doctor"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

// #1288: install and repair must surface the same protocol-unit DNS warning
// `veil doctor` prints, before the operator finds out from clients.

func stubEgressDNSProbe(t *testing.T, check doctorflow.EgressDNSCheck) *int {
	t.Helper()
	calls := 0
	old := doctorEgressDNSProbe
	doctorEgressDNSProbe = func() doctorflow.EgressDNSCheck { calls++; return check }
	t.Cleanup(func() { doctorEgressDNSProbe = old })
	return &calls
}

func readyRootPreflightEnv() preflightEnv {
	return preflightEnv{
		root: true, systemd: true, manager: "apt-get",
		tools: map[string]bool{"systemctl": true, "sysctl": true, "ufw": true, "useradd": true,
			"curl": true, "openssl": true, "socat": true, "crontab": true,
			"tar": true, "gzip": true, "xz": true, "sha256sum": true},
		users: map[string]bool{"veil": true},
	}
}

func TestInstallPreflightEgressDNSCheck(t *testing.T) {
	cases := []struct {
		name       string
		check      doctorflow.EgressDNSCheck
		wantStatus preflightStatus
		wantDetail string
		wantRemedy string
	}{
		{
			name:       "resolved stub reachable",
			check:      doctorflow.EgressDNSCheck{Nameservers: []string{"127.0.0.53"}, Reachable: true},
			wantStatus: preflightOK,
			wantDetail: "host resolver reachable from protocol units (127.0.0.53)",
		},
		{
			name:       "AWS VPC resolver denied",
			check:      doctorflow.EgressDNSCheck{Nameservers: []string{"172.31.0.2", "169.254.169.253"}, PinnedDoH: "1.1.1.1:443"},
			wantStatus: preflightWarn,
			wantDetail: "protocol units cannot reach the host resolver (172.31.0.2, 169.254.169.253)",
			wantRemedy: "Mieru domain lookups fail",
		},
		{
			name:       "missing resolv.conf",
			check:      doctorflow.EgressDNSCheck{Nameservers: []string{}, ResolvConfMissing: true, PinnedDoH: "1.1.1.1:443"},
			wantStatus: preflightWarn,
			wantDetail: "no /etc/resolv.conf",
			wantRemedy: "pin Cloudflare DoH 1.1.1.1:443",
		},
		{
			name:       "unreadable resolv.conf",
			check:      doctorflow.EgressDNSCheck{Nameservers: []string{}, ReadError: "permission denied"},
			wantStatus: preflightWarn,
			wantDetail: "cannot read /etc/resolv.conf",
			wantRemedy: "cannot read /etc/resolv.conf (permission denied)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubPreflightEnv(t, readyRootPreflightEnv())
			stubEgressDNSProbe(t, tc.check)
			profile, opts := directProfileOpts()
			report := collectInstallPreflight(context.Background(), profile, opts)
			c, ok := checkByID(report, "egress-dns")
			if !ok {
				t.Fatalf("no egress-dns check in report:\n%s", report)
			}
			if c.Status != tc.wantStatus || !strings.Contains(c.Detail, tc.wantDetail) {
				t.Fatalf("egress-dns = %+v, want status %s detail containing %q", c, tc.wantStatus, tc.wantDetail)
			}
			if want := doctorflow.EgressDNSWarning(tc.check); c.Remedy != want {
				t.Fatalf("remedy = %q, want doctor's warning %q", c.Remedy, want)
			}
			if tc.wantRemedy != "" && !strings.Contains(c.Remedy, tc.wantRemedy) {
				t.Fatalf("remedy = %q, want it to contain %q", c.Remedy, tc.wantRemedy)
			}
			// Advisory only: an unreachable resolver degrades Mieru, it does
			// not make the host uninstallable.
			if !report.ready() {
				t.Fatalf("egress-dns %s must not block the install:\n%s", c.Status, report)
			}
			rendered := report.String()
			if tc.wantStatus == preflightWarn && !strings.Contains(rendered, "remedy: "+c.Remedy) {
				t.Fatalf("warn remedy not printed:\n%s", rendered)
			}
			if tc.wantStatus == preflightOK && strings.Contains(rendered, "#1288") {
				t.Fatalf("reachable resolver must not print a warning:\n%s", rendered)
			}
		})
	}
}

func TestInstallPreflightEgressDNSOnlyForRootLinux(t *testing.T) {
	unreachable := doctorflow.EgressDNSCheck{Nameservers: []string{"10.0.0.2"}, PinnedDoH: "1.1.1.1:443"}
	t.Run("non-root", func(t *testing.T) {
		env := readyRootPreflightEnv()
		env.root = false
		stubPreflightEnv(t, env)
		calls := stubEgressDNSProbe(t, unreachable)
		profile, opts := directProfileOpts()
		report := collectInstallPreflight(context.Background(), profile, opts)
		if c, ok := checkByID(report, "egress-dns"); ok || *calls != 0 {
			t.Fatalf("non-root install manages no protocol units; got %+v (probe calls %d)", c, *calls)
		}
	})
	t.Run("non-linux", func(t *testing.T) {
		env := readyRootPreflightEnv()
		env.platform = hostenv.Platform{OS: "darwin", Arch: "arm64"}
		stubPreflightEnv(t, env)
		calls := stubEgressDNSProbe(t, unreachable)
		profile, opts := directProfileOpts()
		report := collectInstallPreflight(context.Background(), profile, opts)
		if c, ok := checkByID(report, "egress-dns"); ok || *calls != 0 {
			t.Fatalf("no egress filter off Linux; got %+v (probe calls %d)", c, *calls)
		}
	})
}

func TestRepairDryRunPrintsEgressDNSWarning(t *testing.T) {
	for _, tc := range []struct {
		name  string
		os    string
		check doctorflow.EgressDNSCheck
		want  bool
	}{
		{"unreachable on linux", "linux", doctorflow.EgressDNSCheck{Nameservers: []string{"172.31.0.2"}, PinnedDoH: "1.1.1.1:443"}, true},
		{"reachable on linux", "linux", doctorflow.EgressDNSCheck{Nameservers: []string{"127.0.0.53"}, Reachable: true}, false},
		{"unreachable off linux", "darwin", doctorflow.EgressDNSCheck{Nameservers: []string{"172.31.0.2"}, PinnedDoH: "1.1.1.1:443"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubEgressDNSProbe(t, tc.check)
			oldOS := repairPlatformOS
			repairPlatformOS = func() string { return tc.os }
			t.Cleanup(func() { repairPlatformOS = oldOS })

			dir := t.TempDir()
			cmd := NewRootCommand("test")
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{
				"repair", "--profile", "ru-recommended", "--dry-run",
				"--etc-dir", filepath.Join(dir, "etc", "veil"),
				"--var-dir", filepath.Join(dir, "var", "lib", "veil"),
				"--systemd-dir", filepath.Join(dir, "systemd"),
			})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("repair --dry-run: %v\n%s", err, out.String())
			}
			got := out.String()
			warning := "warning: " + doctorflow.EgressDNSWarning(tc.check)
			if tc.want && !strings.Contains(got, warning) {
				t.Fatalf("repair output missing %q:\n%s", warning, got)
			}
			if !tc.want && strings.Contains(got, "#1288") {
				t.Fatalf("repair output must not warn:\n%s", got)
			}
		})
	}
}
