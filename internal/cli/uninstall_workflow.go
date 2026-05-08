package cli

import (
	"fmt"
	"io"
)

type uninstallWorkflowOptions struct {
	DryRun bool
	Yes    bool
}

type UninstallWorkflow struct {
	opts           uninstallWorkflowOptions
	out            io.Writer
	errOut         io.Writer
	serviceStopper func(string) error
	fileRemover    func(string) error
}

func NewUninstallWorkflow(opts uninstallWorkflowOptions, out io.Writer, errOut io.Writer) UninstallWorkflow {
	return UninstallWorkflow{
		opts:           opts,
		out:            out,
		errOut:         errOut,
		serviceStopper: uninstallServiceStopper,
		fileRemover:    uninstallFileRemover,
	}
}

func (w UninstallWorkflow) Run() error {
	plan := uninstallPlan()
	fmt.Fprintln(w.out, "Veil uninstall plan")
	fmt.Fprintln(w.out, plan)
	if w.opts.DryRun {
		return nil
	}
	if !w.opts.Yes {
		return fmt.Errorf("uninstall requires --yes; rerun with --dry-run to preview")
	}
	for _, svc := range uninstallServices() {
		if err := w.serviceStopper(svc); err != nil {
			fmt.Fprintf(w.errOut, "warning: service %s: %v\n", svc, err)
		}
	}
	for _, path := range uninstallPaths() {
		if err := w.fileRemover(path); err != nil {
			fmt.Fprintf(w.errOut, "warning: remove %s: %v\n", path, err)
		}
	}
	fmt.Fprintln(w.out, "Uninstalled Veil")
	return nil
}

func uninstallServices() []string {
	return []string{"veil.service", "veil-naive.service", "veil-hysteria2.service", "veil-warp.service", "veil-mieru.service"}
}

func uninstallPaths() []string {
	paths := []string{"/etc/veil", "/var/lib/veil"}
	paths = append(paths, uninstallSystemdUnitPaths()...)
	paths = append(paths, "/usr/local/bin/veil")
	return paths
}

func uninstallSystemdUnitPaths() []string {
	return []string{
		"/etc/systemd/system/veil.service",
		"/etc/systemd/system/veil-naive.service",
		"/etc/systemd/system/veil-hysteria2.service",
		"/etc/systemd/system/veil-warp.service",
		"/etc/systemd/system/veil-mieru.service",
	}
}
