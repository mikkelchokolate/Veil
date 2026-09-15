package uninstall

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/systemdunits"
)

const (
	backupScheduleDropInDir = "veil-backup.service.d"
	caddyStateDirDefault    = "/var/lib/caddy"
	mitaStateDirDefault     = "/var/lib/mita"
)

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
	return opts
}

func Paths(opts Options) []string {
	opts = opts.WithDefaults()
	paths := []string{}
	if !opts.PreservesData() {
		paths = append(paths, stateDataPaths(opts)...)
	}
	paths = append(paths, SystemdUnitPaths(opts)...)
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
	paths := make([]string, 0, len(units)+2)
	for _, name := range units {
		paths = append(paths, filepath.ToSlash(filepath.Join(opts.SystemdDir, name)))
	}
	paths = append(paths, filepath.ToSlash(filepath.Join(opts.SystemdDir, backupScheduleDropInDir)))
	paths = append(paths, TemplateInstancePaths(opts)...)
	return paths
}

func TemplateInstancePaths(opts Options) []string {
	opts = opts.WithDefaults()
	wantsDir := filepath.Join(opts.SystemdDir, "multi-user.target.wants")
	var paths []string
	for _, name := range systemdunits.Names() {
		glob := templateInstanceWantsGlob(name)
		if glob == "" {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(wantsDir, glob))
		if err != nil {
			continue
		}
		for _, match := range matches {
			paths = append(paths, filepath.ToSlash(match))
		}
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
