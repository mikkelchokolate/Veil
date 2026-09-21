package uninstall

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

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
}

func Run(opts Options, out io.Writer, errOut io.Writer, deps Dependencies) error {
	opts = opts.WithDefaults()
	fmt.Fprintln(out, "Veil uninstall plan")
	fmt.Fprintln(out, Plan(opts))
	if opts.DryRun {
		return nil
	}
	if !opts.Yes {
		return fmt.Errorf("uninstall requires --yes; rerun with --dry-run to preview")
	}
	for _, svc := range Services() {
		if err := deps.ServiceStopper(svc); err != nil {
			fmt.Fprintf(errOut, "warning: service %s: %v\n", svc, err)
		}
	}
	for _, svc := range TemplateInstanceUnits(opts) {
		if err := deps.ServiceStopper(svc); err != nil {
			fmt.Fprintf(errOut, "warning: service %s: %v\n", svc, err)
		}
	}
	for _, path := range Paths(opts) {
		if err := deps.FileRemover(path); err != nil {
			fmt.Fprintf(errOut, "warning: remove %s: %v\n", path, err)
		}
	}
	if err := deps.SystemdReloader(); err != nil {
		fmt.Fprintf(errOut, "warning: systemd daemon-reload: %v\n", err)
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
	b.WriteString("Remove systemd units:\n")
	for _, path := range SystemdUnitPaths(opts) {
		b.WriteString(fmt.Sprintf("  - %s\n", path))
	}
	b.WriteString("Remove sysctl drop-in:\n")
	b.WriteString(fmt.Sprintf("  - %s\n", filepath.ToSlash(opts.SysctlConfPath)))
	b.WriteString("Remove binary:\n")
	b.WriteString(fmt.Sprintf("  - %s\n", BinaryPath(opts)))
	return b.String()
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
