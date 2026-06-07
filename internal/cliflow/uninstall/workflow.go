package uninstall

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/renderer"
)

type Options struct {
	DryRun     bool
	Yes        bool
	Purge      bool
	KeepData   bool
	EtcDir     string
	VarDir     string
	SystemdDir string
	InstallDir string
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
		b.WriteString(fmt.Sprintf("  - %s\n", opts.EtcDir))
		b.WriteString(fmt.Sprintf("  - %s\n", opts.VarDir))
	} else {
		b.WriteString("Remove configuration and state:\n")
		for _, path := range []string{opts.EtcDir, opts.VarDir} {
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
	return renderer.ManagedSystemdUnitNames()
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
	return opts
}

func Paths(opts Options) []string {
	opts = opts.WithDefaults()
	paths := []string{}
	if !opts.PreservesData() {
		paths = append(paths, filepath.ToSlash(opts.EtcDir), filepath.ToSlash(opts.VarDir))
	}
	paths = append(paths, SystemdUnitPaths(opts)...)
	paths = append(paths, BinaryPath(opts))
	return paths
}

func SystemdUnitPaths(opts Options) []string {
	opts = opts.WithDefaults()
	paths := make([]string, 0, len(renderer.ManagedSystemdUnitNames()))
	for _, name := range renderer.ManagedSystemdUnitNames() {
		paths = append(paths, filepath.ToSlash(filepath.Join(opts.SystemdDir, name)))
	}
	return paths
}

func BinaryPath(opts Options) string {
	opts = opts.WithDefaults()
	return filepath.ToSlash(filepath.Join(opts.InstallDir, "veil"))
}
