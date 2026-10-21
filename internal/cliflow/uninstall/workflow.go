package uninstall

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/mikkelchokolate/Veil/internal/systemdunits"
)

const (
	caddyStateDirDefault = "/var/lib/caddy"
	mitaStateDirDefault  = "/var/lib/mita"
	// quicSysctlConfDefault is the packaged QUIC buffer drop-in. Uninstall
	// removes it so the host does not keep Veil's sysctl overrides after the
	// software is gone (issue #486).
	quicSysctlConfDefault = "/etc/sysctl.d/99-veil-quic.conf"
)

// defaultVendorSystemdDirs are the packaged (vendor) unit directories the
// nfpm packages install into — distinct from Options.SystemdDir, which holds
// the units `veil install` writes. /lib/systemd is a symlink to /usr/lib on
// usrmerge distros, but listing both keeps removal honest everywhere (#492).
var defaultVendorSystemdDirs = []string{"/lib/systemd/system", "/usr/lib/systemd/system"}

// legacyInstanceUnitGlobs cover pre-consolidation per-instance units that are
// no longer in the runtime catalog but stay stopped-enabled on hosts upgraded
// from per-inbound Caddy — the apply orphan scan covers the same "veil-caddy@"
// prefix, and uninstall must match it (issue #375).
var legacyInstanceUnitGlobs = []string{"veil-caddy@*.service"}

// firewallPlanLine describes the UFW cleanup in the uninstall plan. The
// management SSH allow survives so an active UFW does not lock the operator
// out (#1289).
const firewallPlanLine = `UFW allow rules commented "Veil …" (panel, proxy ports, ACME challenge); "Veil management SSH" is kept`

type Options struct {
	DryRun        bool
	Yes           bool
	Purge         bool
	KeepData      bool
	EtcDir        string
	VarDir        string
	SystemdDir    string
	InstallDir    string
	CaddyStateDir string
	MitaStateDir  string
	// VendorSystemdDirs lists packaged unit directories to also clean
	// (defaults to /lib/systemd/system and /usr/lib/systemd/system).
	VendorSystemdDirs []string
	// SysctlConfPath is the QUIC sysctl drop-in removed on uninstall.
	SysctlConfPath string
}

// PreservesData reports whether configuration and state directories are kept.
// Uninstall removes them by default so a subsequent install starts fresh;
// --keep-data preserves them, and an explicit --purge always removes them.
func (opts Options) PreservesData() bool {
	return opts.KeepData && !opts.Purge
}

type Dependencies struct {
	ServiceStopper  func(string) error
	FileRemover     func(string) error
	SystemdReloader func() error
	// FirewallCleaner deletes Veil-managed UFW allow rules (every "Veil …"
	// comment except the management SSH allow) and returns the deleted
	// targets. It runs only on a full removal: --keep-data keeps the rules so
	// a reinstall that reuses the configuration keeps its ports open
	// (#1289). Nil skips the step.
	FirewallCleaner func() ([]string, error)
}

func Run(opts Options, out io.Writer, errOut io.Writer, deps Dependencies) error {
	opts = opts.WithDefaults()
	if err := ValidateSysctlConfPath(opts.SysctlConfPath); err != nil {
		return err
	}
	fmt.Fprintln(out, "Veil uninstall plan")
	fmt.Fprintln(out, Plan(opts))
	if opts.DryRun {
		return nil
	}
	if !opts.Yes {
		return fmt.Errorf("uninstall requires --yes; rerun with --dry-run to preview")
	}
	// Every step still runs so the cleanup attempt is maximal, but failures
	// are counted and the command fails closed afterwards: units that stayed
	// enabled or files that could not be removed must never be reported as a
	// successful uninstall (issue #1026).
	failures := 0
	warn := func(format string, args ...any) {
		failures++
		fmt.Fprintf(errOut, "warning: "+format+"\n", args...)
	}
	for _, svc := range Services() {
		if err := deps.ServiceStopper(svc); err != nil {
			warn("service %s: %v", svc, err)
		}
	}
	for _, svc := range TemplateInstanceUnits(opts) {
		if err := deps.ServiceStopper(svc); err != nil {
			warn("service %s: %v", svc, err)
		}
	}
	if !opts.PreservesData() && deps.FirewallCleaner != nil {
		// Services are already stopped, so nothing Veil-managed listens on
		// these ports any more. Leaving the allows would expose whatever
		// binds them next (#1289).
		removed, err := deps.FirewallCleaner()
		for _, target := range removed {
			fmt.Fprintf(out, "Removed Veil firewall rule %s\n", target)
		}
		if err != nil {
			warn("firewall: %v", err)
		}
	}
	for _, path := range Paths(opts) {
		if err := deps.FileRemover(path); err != nil {
			warn("remove %s: %v", path, err)
		}
	}
	if err := deps.SystemdReloader(); err != nil {
		warn("systemd daemon-reload: %v", err)
	}
	if failures > 0 {
		fmt.Fprintf(errOut, "Veil uninstall incomplete: %d step(s) failed; units may still be enabled and files may remain. Re-run after resolving the errors above.\n", failures)
		return fmt.Errorf("uninstall incomplete: %d step(s) failed", failures)
	}
	fmt.Fprintln(out, "Uninstalled Veil")
	if opts.PreservesData() {
		fmt.Fprintf(out, "Kept configuration and state in %s and %s; the next install reuses the existing admin login and panel path.\n", opts.EtcDir, opts.VarDir)
	} else {
		fmt.Fprintf(out, "Removed configuration and state in %s and %s; the next install generates a fresh password and panel path.\n", opts.EtcDir, opts.VarDir)
		fmt.Fprintln(out, "Re-run with --keep-data to preserve credentials and configuration across reinstalls.")
	}
	return nil
}

func Plan(opts Options) string {
	var b strings.Builder
	b.WriteString("Stop services:\n")
	for _, svc := range Services() {
		b.WriteString(fmt.Sprintf("  - %s\n", svc))
	}
	b.WriteString("Disable services:\n")
	for _, svc := range Services() {
		b.WriteString(fmt.Sprintf("  - %s\n", svc))
	}
	opts = opts.WithDefaults()
	if opts.PreservesData() {
		b.WriteString("Preserved state:\n")
		for _, path := range stateDataPaths(opts) {
			b.WriteString(fmt.Sprintf("  - %s\n", path))
		}
	} else {
		b.WriteString("Remove configuration and state:\n")
		for _, path := range stateDataPaths(opts) {
			b.WriteString(fmt.Sprintf("  - %s\n", path))
		}
	}
	if opts.PreservesData() {
		b.WriteString("Preserved firewall rules:\n")
		b.WriteString("  - Veil-managed UFW allow rules (kept with --keep-data)\n")
	} else {
		b.WriteString("Remove firewall rules:\n")
		b.WriteString("  - " + firewallPlanLine + "\n")
	}
	b.WriteString("Remove systemd units:\n")
	for _, path := range SystemdUnitPaths(opts) {
		b.WriteString(fmt.Sprintf("  - %s\n", path))
	}
	b.WriteString("Remove sysctl drop-in:\n")
	b.WriteString(fmt.Sprintf("  - %s\n", filepath.ToSlash(opts.SysctlConfPath)))
	b.WriteString("Remove binary:\n")
	b.WriteString(fmt.Sprintf("  - %s\n", BinaryPath(opts)))
	if opts.PreservesData() {
		b.WriteString("Preserved runtime binaries:\n")
		b.WriteString("  - runtimes Veil installed into " + filepath.ToSlash(opts.InstallDir) + " (kept with --keep-data)\n")
		return b.String()
	}
	b.WriteString("Remove runtime binaries installed by Veil:\n")
	runtimePaths := RuntimeBinaryPaths(opts)
	if len(runtimePaths) == 0 {
		b.WriteString("  - none recorded (no Veil runtime provenance in " + filepath.ToSlash(opts.InstallDir) + ")\n")
	}
	for _, path := range runtimePaths {
		b.WriteString(fmt.Sprintf("  - %s\n", path))
	}
	return b.String()
}

// QUICSysctlConfName is the only file name uninstall will delete as the
// sysctl drop-in.
const QUICSysctlConfName = "99-veil-quic.conf"

// ValidateSysctlConfPath gates the sysctl drop-in target before it reaches
// the remover. The path is overridable (VEIL_SYSCTL_CONF) for tests and
// alternate layouts, but it is removed unconditionally and as root, and
// ensureRemovablePath passes regular files straight through — so
// VEIL_SYSCTL_CONF=/etc/important.conf would delete an arbitrary file
// (issue #1263). Only a file named 99-veil-quic.conf (the drop-in Veil
// writes) is accepted; anything else fails closed before any step runs.
func ValidateSysctlConfPath(path string) error {
	if filepath.Base(filepath.Clean(path)) != QUICSysctlConfName {
		return fmt.Errorf("refusing to use sysctl drop-in %s: uninstall only removes a file named %s (check VEIL_SYSCTL_CONF)", path, QUICSysctlConfName)
	}
	return nil
}

func Services() []string {
	return systemdunits.Names()
}

func (opts Options) WithDefaults() Options {
	if opts.EtcDir == "" {
		opts.EtcDir = "/etc/veil"
	}
	if opts.VarDir == "" {
		opts.VarDir = "/var/lib/veil"
	}
	if opts.SystemdDir == "" {
		opts.SystemdDir = "/etc/systemd/system"
	}
	if opts.InstallDir == "" {
		opts.InstallDir = "/usr/local/bin"
	}
	if opts.CaddyStateDir == "" {
		opts.CaddyStateDir = caddyStateDirDefault
	}
	if opts.MitaStateDir == "" {
		opts.MitaStateDir = mitaStateDirDefault
	}
	if len(opts.VendorSystemdDirs) == 0 {
		opts.VendorSystemdDirs = defaultVendorSystemdDirs
	}
	if opts.SysctlConfPath == "" {
		opts.SysctlConfPath = quicSysctlConfDefault
	}
	return opts
}

func Paths(opts Options) []string {
	opts = opts.WithDefaults()
	paths := []string{}
	if !opts.PreservesData() {
		paths = append(paths, stateDataPaths(opts)...)
	}
	paths = append(paths, SystemdUnitPaths(opts)...)
	// The packaged QUIC sysctl drop-in is host-level, not user state — it is
	// removed even under --keep-data (issue #486).
	paths = append(paths, filepath.ToSlash(opts.SysctlConfPath))
	paths = append(paths, BinaryPath(opts))
	// Runtime binaries (caddy, hysteria, sing-box, mita, olcrtc) go on a full
	// removal so a later install starts fresh instead of adopting stale
	// binaries left on PATH (#1289). --keep-data keeps them: `veil install`
	// does not fetch runtimes, so an in-place upgrade that reuses the
	// configuration must find its protocol binaries still in place.
	if !opts.PreservesData() {
		paths = append(paths, RuntimeBinaryPaths(opts)...)
	}
	return paths
}

// RuntimeBinaryPaths lists the runtime binaries Veil's installer wrote into
// the install dir, plus its provenance bookkeeping. Only files whose current
// bytes match the digest recorded at install time are included, so a system
// or operator-provided caddy at the same path is never removed (#1289).
func RuntimeBinaryPaths(opts Options) []string {
	opts = opts.WithDefaults()
	artifacts := runtimeinstall.ManagedRuntimeArtifacts(opts.InstallDir)
	paths := make([]string, 0, len(artifacts))
	for _, path := range artifacts {
		paths = append(paths, filepath.ToSlash(path))
	}
	return paths
}

func stateDataPaths(opts Options) []string {
	opts = opts.WithDefaults()
	return []string{
		filepath.ToSlash(opts.EtcDir),
		filepath.ToSlash(opts.VarDir),
		filepath.ToSlash(opts.CaddyStateDir),
		filepath.ToSlash(opts.MitaStateDir),
	}
}

func SystemdUnitPaths(opts Options) []string {
	opts = opts.WithDefaults()
	units := systemdunits.Names()
	// Clean both the installer's unit directory and the packaged vendor
	// directories — `dpkg -r` leftovers or a partially removed package must not
	// survive `veil uninstall` (issue #492).
	dirs := append([]string{opts.SystemdDir}, opts.VendorSystemdDirs...)
	paths := make([]string, 0, len(dirs)*(len(units)+1))
	seen := map[string]bool{}
	add := func(path string) {
		if seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	for _, dir := range dirs {
		for _, name := range units {
			add(filepath.ToSlash(filepath.Join(dir, name)))
			// Packaged installs with custom --etc-dir/--var-dir record the
			// layout in <unit>.d/10-veil-install.conf drop-ins, and
			// veil-backup.service.d also holds the schedule drop-in
			// (passphrase-path.conf). Deleting the unit file while leaving
			// its drop-in lets the stale override merge into the next
			// package install — the whole drop-in dir goes (issue #642).
			add(filepath.ToSlash(filepath.Join(dir, name+".d")))
		}
		// Dir-level template globs can re-match the catalog template file
		// itself (e.g. veil-hysteria2@.service); dedupe keeps the plan and the
		// removal list honest.
		for _, path := range templateInstancePaths(dir) {
			add(path)
		}
	}
	return paths
}

func TemplateInstancePaths(opts Options) []string {
	opts = opts.WithDefaults()
	var paths []string
	for _, dir := range append([]string{opts.SystemdDir}, opts.VendorSystemdDirs...) {
		paths = append(paths, templateInstancePaths(dir)...)
	}
	return paths
}

func templateInstancePaths(systemdDir string) []string {
	wantsDir := filepath.Join(systemdDir, "multi-user.target.wants")
	var globs []string
	for _, name := range systemdunits.Names() {
		if glob := templateInstanceWantsGlob(name); glob != "" {
			globs = append(globs, glob)
		}
	}
	globs = append(globs, legacyInstanceUnitGlobs...)
	var paths []string
	seen := map[string]bool{}
	addMatches := func(pattern string) {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return
		}
		for _, match := range matches {
			slashed := filepath.ToSlash(match)
			if seen[slashed] {
				continue
			}
			seen[slashed] = true
			paths = append(paths, slashed)
		}
	}
	for _, glob := range globs {
		// Enablement wants links AND concrete per-instance unit files: legacy
		// veil-caddy@ hosts can carry either, and a dangling wants link or a
		// stray instance file must not survive uninstall (issue #375).
		addMatches(filepath.Join(wantsDir, glob))
		addMatches(filepath.Join(systemdDir, glob))
	}
	return paths
}

func TemplateInstanceUnits(opts Options) []string {
	paths := TemplateInstancePaths(opts)
	units := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		name := filepath.Base(path)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		units = append(units, name)
	}
	return units
}

func templateInstanceWantsGlob(unit string) string {
	const suffix = "@.service"
	if strings.HasSuffix(unit, suffix) {
		return strings.TrimSuffix(unit, suffix) + "@*.service"
	}
	return ""
}

func BinaryPath(opts Options) string {
	opts = opts.WithDefaults()
	return filepath.ToSlash(filepath.Join(opts.InstallDir, "veil"))
}
