package cli

import (
	"fmt"
	"io"
	"path/filepath"
)

type uninstallWorkflowOptions struct {
	DryRun     bool
	Yes        bool
	EtcDir     string
	VarDir     string
	SystemdDir string
	InstallDir string
}

type UninstallWorkflow struct {
	opts            uninstallWorkflowOptions
	out             io.Writer
	errOut          io.Writer
	serviceStopper  func(string) error
	fileRemover     func(string) error
	systemdReloader func() error
}

func NewUninstallWorkflow(opts uninstallWorkflowOptions, out io.Writer, errOut io.Writer) UninstallWorkflow {
	return UninstallWorkflow{
		opts:            opts,
		out:             out,
		errOut:          errOut,
		serviceStopper:  uninstallServiceStopper,
		fileRemover:     uninstallFileRemover,
		systemdReloader: uninstallSystemdReloader,
	}
}

func (w UninstallWorkflow) Run() error {
	opts := w.opts.withDefaults()
	plan := uninstallPlan(opts)
	fmt.Fprintln(w.out, "Veil uninstall plan")
	fmt.Fprintln(w.out, plan)
	if opts.DryRun {
		return nil
	}
	if !opts.Yes {
		return fmt.Errorf("uninstall requires --yes; rerun with --dry-run to preview")
	}
	for _, svc := range uninstallServices() {
		if err := w.serviceStopper(svc); err != nil {
			fmt.Fprintf(w.errOut, "warning: service %s: %v\n", svc, err)
		}
	}
	for _, path := range uninstallPaths(opts) {
		if err := w.fileRemover(path); err != nil {
			fmt.Fprintf(w.errOut, "warning: remove %s: %v\n", path, err)
		}
	}
	if err := w.systemdReloader(); err != nil {
		fmt.Fprintf(w.errOut, "warning: systemd daemon-reload: %v\n", err)
	}
	fmt.Fprintln(w.out, "Uninstalled Veil")
	return nil
}

func uninstallServices() []string {
	return []string{"veil.service", "veil-naive.service", "veil-hysteria2.service", "veil-warp.service", "veil-mieru.service"}
}

func (opts uninstallWorkflowOptions) withDefaults() uninstallWorkflowOptions {
	if opts.EtcDir == "" {
		opts.EtcDir = "/etc/veil"
	}
	if opts.VarDir == "" {
		opts.VarDir = "/var/lib/veil"
	}
	if opts.SystemdDir == "" {
		opts.SystemdDir = defaultSystemdDir
	}
	if opts.InstallDir == "" {
		opts.InstallDir = "/usr/local/bin"
	}
	return opts
}

func uninstallPaths(opts uninstallWorkflowOptions) []string {
	opts = opts.withDefaults()
	paths := []string{opts.EtcDir, opts.VarDir}
	paths = append(paths, uninstallSystemdUnitPaths(opts)...)
	paths = append(paths, uninstallBinaryPath(opts))
	return paths
}

func uninstallSystemdUnitPaths(opts uninstallWorkflowOptions) []string {
	opts = opts.withDefaults()
	return []string{
		filepath.Join(opts.SystemdDir, "veil.service"),
		filepath.Join(opts.SystemdDir, "veil-naive.service"),
		filepath.Join(opts.SystemdDir, "veil-hysteria2.service"),
		filepath.Join(opts.SystemdDir, "veil-warp.service"),
		filepath.Join(opts.SystemdDir, "veil-mieru.service"),
	}
}

func uninstallBinaryPath(opts uninstallWorkflowOptions) string {
	opts = opts.withDefaults()
	return filepath.Join(opts.InstallDir, "veil")
}
