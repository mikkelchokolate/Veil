package cli

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/installer"
)

// Audit #304: one platform-aware prerequisite inventory for the whole install.
// collectInstallPreflight inspects the host WITHOUT mutating it; the workflow
// prints the report, refuses unsupported/missing requirements before any
// download or state write, and provisions the rest in a single explicit phase
// after the operator has accepted the plan.

type preflightStatus string

const (
	preflightOK        preflightStatus = "ok"
	preflightProvision preflightStatus = "provision" // absent, auto-provisionable
	preflightMissing   preflightStatus = "missing"   // required, cannot provision
	preflightSkipped   preflightStatus = "skipped"   // not needed for this install
)

type preflightCheck struct {
	ID      string
	Status  preflightStatus
	Detail  string
	Remedy  string
	Package string // distro package used when Status == provision
}

type installPreflightReport struct {
	Root   bool
	Checks []preflightCheck
}

func (r installPreflightReport) ready() bool {
	for _, c := range r.Checks {
		if c.Status == preflightMissing {
			return false
		}
	}
	return true
}

// String renders the human-readable capability/readiness report.
func (r installPreflightReport) String() string {
	var b strings.Builder
	mode := "root (system services)"
	if !r.Root {
		mode = "non-root (host-level steps skipped)"
	}
	fmt.Fprintf(&b, "Install capability report [%s]\n", mode)
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 40))
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "%-22s %-9s %s\n", c.ID, c.Status, c.Detail)
		if c.Remedy != "" && (c.Status == preflightMissing || c.Status == preflightProvision) {
			fmt.Fprintf(&b, "%-22s %-9s %s\n", "", "", "remedy: "+c.Remedy)
		}
	}
	return b.String()
}

// supportedPackageManagers mirrors distroManagers in acmeip: the first found
// manager provisions missing prerequisites.
var installSupportedManagers = []struct {
	name string
	args func(pkgs ...string) []string
}{
	{"apt-get", func(pkgs ...string) []string { return append([]string{"install", "-y"}, pkgs...) }},
	{"dnf", func(pkgs ...string) []string { return append([]string{"-y", "install"}, pkgs...) }},
	{"yum", func(pkgs ...string) []string { return append([]string{"-y", "install"}, pkgs...) }},
	{"pacman", func(pkgs ...string) []string { return append([]string{"-Sy", "--noconfirm"}, pkgs...) }},
	{"zypper", func(pkgs ...string) []string { return append([]string{"-q", "install", "-y"}, pkgs...) }},
	{"apk", func(pkgs ...string) []string { return append([]string{"add"}, pkgs...) }},
}

// installPlatformFunc reports the host platform; tests stub it for the
// supported-matrix check.
var installPlatformFunc = hostenv.CurrentPlatform

// installPkgManagerFunc detects the host package manager; "" when unsupported.
var installPkgManagerFunc = func() string {
	for _, m := range installSupportedManagers {
		if _, err := execLookPath(m.name); err == nil {
			return m.name
		}
	}
	return ""
}

// installUserLookupFunc reports whether a system account exists; tests stub it.
var installUserLookupFunc = func(name string) bool {
	_, err := user.Lookup(name)
	return err == nil
}

// installPathExistsFunc reports whether a path exists; tests stub it.
var installPathExistsFunc = func(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// installSystemdPresentFunc reports a live systemd init; tests stub it.
var installSystemdPresentFunc = func() bool {
	if _, err := execLookPath("systemctl"); err != nil {
		return false
	}
	return installPathExistsFunc("/run/systemd/system")
}

// distroPackageFor maps a logical prerequisite to the package providing it for
// the detected manager ("" = the manager cannot supply it).
func distroPackageFor(prereq, manager string) string {
	switch prereq {
	case "accounts":
		switch manager {
		case "apt-get":
			return "passwd"
		case "apk":
			return "busybox"
		default:
			return "shadow-utils"
		}
	case "scheduler":
		switch manager {
		case "apt-get":
			return "cron"
		case "apk":
			return "dcron"
		default:
			return "cronie"
		}
	case "sysctl":
		if manager == "dnf" || manager == "yum" {
			return "procps-ng"
		}
		return "procps"
	default:
		// ufw, openssl, socat, curl, tar, xz, gzip use upstream names.
		return prereq
	}
}

func anyLookPath(names ...string) (string, bool) {
	for _, n := range names {
		if _, err := execLookPath(n); err == nil {
			return n, true
		}
	}
	return "", false
}

// collectInstallPreflight inventories every host prerequisite the install will
// exercise. Read-only: no installs, no writes.
func collectInstallPreflight(ctx context.Context, profile installer.RURecommendedProfile, opts ruRecommendedInstallOptions) installPreflightReport {
	if ctx == nil {
		ctx = context.Background()
	}
	root := installGeteuidFunc() == 0
	report := installPreflightReport{Root: root}
	manager := ""
	if root {
		manager = installPkgManagerFunc()
	}
	provision := func(prereq string) (preflightStatus, string) {
		if manager == "" {
			return preflightMissing, "no supported package manager (apt-get/dnf/yum/pacman/zypper/apk) to provision it"
		}
		return preflightProvision, ""
	}
	pkgFor := func(prereq string) string {
		if manager == "" {
			return ""
		}
		return distroPackageFor(prereq, manager)
	}
	add := func(c preflightCheck) { report.Checks = append(report.Checks, c) }

	// 1. Platform support: linux on amd64/arm64 (the pinned runtime matrix).
	platform := installPlatformFunc()
	if platform.OS != "linux" {
		add(preflightCheck{ID: "platform", Status: preflightMissing,
			Detail: fmt.Sprintf("%s/%s is unsupported", platform.OS, platform.Arch),
			Remedy: "install on a supported Linux host (systemd, amd64/arm64)"})
	} else if platform.Arch != "amd64" && platform.Arch != "arm64" {
		add(preflightCheck{ID: "platform", Status: preflightMissing,
			Detail: fmt.Sprintf("unsupported architecture %s", platform.Arch),
			Remedy: "supported architectures: x86_64/amd64, aarch64/arm64"})
	} else {
		add(preflightCheck{ID: "platform", Status: preflightOK,
			Detail: fmt.Sprintf("linux/%s", platform.Arch)})
	}

	// Host-level checks apply to root installs only; non-root installs skip
	// systemd/firewall/account mutation entirely.
	if !root {
		add(preflightCheck{ID: "init", Status: preflightSkipped, Detail: "non-root install: system services not managed"})
		add(preflightCheck{ID: "accounts", Status: preflightSkipped, Detail: "non-root install: service accounts not created"})
		add(preflightCheck{ID: "firewall", Status: preflightSkipped, Detail: "non-root install: firewall rules not applied"})
	} else {
		// 2. Init system: units are rendered for systemd.
		if installSystemdPresentFunc() {
			add(preflightCheck{ID: "init", Status: preflightOK, Detail: "systemd"})
		} else {
			add(preflightCheck{ID: "init", Status: preflightMissing,
				Detail: "systemd is not the running init system",
				Remedy: "Veil services need systemd; install on a systemd distro or run a non-root install"})
		}

		// 3. Service accounts: veil/veil-proxy are created by the install or
		//    the package postinstall; account tools must exist or be
		//    provisionable (audit #303).
		if installUserLookupFunc("veil") {
			add(preflightCheck{ID: "accounts", Status: preflightOK, Detail: "veil account exists"})
		} else if _, ok := anyLookPath("useradd", "adduser"); ok {
			add(preflightCheck{ID: "accounts", Status: preflightOK, Detail: "account tools present"})
		} else {
			status, remedy := provision("accounts")
			add(preflightCheck{ID: "accounts", Status: status, Detail: "no useradd/adduser for service accounts",
				Remedy:  remedy,
				Package: pkgFor("accounts")})
		}

		// 4. Firewall backend (audit #295): only when the plan opens ports.
		plan, err := buildInstallPlan(profile, opts)
		firewallNeeded := err == nil && len(plan.FirewallActions) > 0
		switch {
		case !firewallNeeded:
			add(preflightCheck{ID: "firewall", Status: preflightSkipped, Detail: "no firewall actions in plan"})
		case func() bool { _, ok := anyLookPath("ufw"); return ok }():
			add(preflightCheck{ID: "firewall", Status: preflightOK, Detail: "ufw"})
		default:
			if active := activeForeignFirewallFunc(ctx); active != "" {
				add(preflightCheck{ID: "firewall", Status: preflightMissing,
					Detail: fmt.Sprintf("competing firewall %q is active", active),
					Remedy: fmt.Sprintf("open the planned ports in %s or disable it, then re-run install", active)})
			} else {
				status, remedy := provision("ufw")
				add(preflightCheck{ID: "firewall", Status: status, Detail: "ufw required to open panel/ACME ports",
					Remedy:  remedy,
					Package: pkgFor("ufw")})
			}
		}

		// 5. sysctl for QUIC buffer tuning.
		if _, ok := anyLookPath("sysctl"); ok {
			add(preflightCheck{ID: "sysctl", Status: preflightOK, Detail: "sysctl present"})
		} else {
			status, remedy := provision("sysctl")
			add(preflightCheck{ID: "sysctl", Status: status, Detail: "sysctl needed for QUIC UDP buffers",
				Remedy:  remedy,
				Package: pkgFor("sysctl")})
		}
	}

	// 6. ACME prerequisites — only for direct mode with LE IP certs (audit #298).
	if profile.PanelAccess == "direct" && opts.LEIPCert {
		for _, tool := range []struct{ name, prereq string }{
			{"curl", "curl"},
			{"openssl", "openssl"},
			{"socat", "socat"},
			{"crontab", "scheduler"},
		} {
			if _, ok := anyLookPath(tool.name); ok {
				add(preflightCheck{ID: "acme-" + tool.name, Status: preflightOK, Detail: tool.name + " present"})
				continue
			}
			status, remedy := provision(tool.prereq)
			add(preflightCheck{ID: "acme-" + tool.name, Status: status, Detail: tool.name + " required for ACME issuance",
				Remedy:  remedy,
				Package: pkgFor(tool.prereq)})
		}
	} else {
		add(preflightCheck{ID: "acme", Status: preflightSkipped, Detail: "no ACME issuance for this profile"})
	}

	// 7. Archive/checksum tools for runtime downloads and verification.
	for _, tool := range []string{"tar", "gzip", "xz", "sha256sum"} {
		if _, ok := anyLookPath(tool); ok {
			add(preflightCheck{ID: "tool-" + tool, Status: preflightOK, Detail: tool + " present"})
			continue
		}
		status, remedy := provision(tool)
		pkg := tool
		if tool == "sha256sum" {
			pkg = "coreutils"
		}
		if manager != "" {
			pkg = distroPackageFor(pkg, manager)
		}
		add(preflightCheck{ID: "tool-" + tool, Status: status, Detail: tool + " required for runtime extraction",
			Remedy:  remedy,
			Package: pkg})
	}

	// 8. Caddy binary — the runtime phase provisions the pinned build when the
	//    profile needs it, so absence is provisionable without a package.
	if profile.InstallPanelCaddy {
		if _, ok := anyLookPath("caddy"); ok {
			add(preflightCheck{ID: "caddy", Status: preflightOK, Detail: "caddy present"})
		} else {
			add(preflightCheck{ID: "caddy", Status: preflightProvision, Detail: "pinned caddy is provisioned by the runtime phase"})
		}
	}
	return report
}

// provisionInstallPreflight installs the packages the report marked
// provisionable through the detected package manager. It only runs for root
// installs — non-root reports mark host-level checks skipped and return early.
func provisionInstallPreflight(ctx context.Context, report installPreflightReport) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !report.Root {
		return nil
	}
	manager := installPkgManagerFunc()
	if manager == "" {
		return nil
	}
	var pkgs []string
	seen := map[string]bool{}
	for _, c := range report.Checks {
		if c.Status == preflightProvision && c.Package != "" && !seen[c.Package] {
			seen[c.Package] = true
			pkgs = append(pkgs, c.Package)
		}
	}
	if len(pkgs) == 0 {
		return nil
	}
	for _, m := range installSupportedManagers {
		if m.name == manager {
			if err := provisionUFWCommandFunc(ctx, m.name, m.args(pkgs...)...); err != nil {
				return fmt.Errorf("provision prerequisites %v via %s: %w", pkgs, manager, err)
			}
			return nil
		}
	}
	return nil
}
