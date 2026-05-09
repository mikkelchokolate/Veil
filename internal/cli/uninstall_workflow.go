package cli

import (
	"io"

	uninstallflow "github.com/veil-panel/veil/internal/cliflow/uninstall"
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
	return uninstallflow.Run(toUninstallFlowOptions(w.opts), w.out, w.errOut, uninstallflow.Dependencies{
		ServiceStopper:  w.serviceStopper,
		FileRemover:     w.fileRemover,
		SystemdReloader: w.systemdReloader,
	})
}

func uninstallServices() []string {
	return uninstallflow.Services()
}

func (opts uninstallWorkflowOptions) withDefaults() uninstallWorkflowOptions {
	return fromUninstallFlowOptions(toUninstallFlowOptions(opts).WithDefaults())
}

func uninstallPaths(opts uninstallWorkflowOptions) []string {
	return uninstallflow.Paths(toUninstallFlowOptions(opts))
}

func uninstallSystemdUnitPaths(opts uninstallWorkflowOptions) []string {
	return uninstallflow.SystemdUnitPaths(toUninstallFlowOptions(opts))
}

func uninstallBinaryPath(opts uninstallWorkflowOptions) string {
	return uninstallflow.BinaryPath(toUninstallFlowOptions(opts))
}

func toUninstallFlowOptions(opts uninstallWorkflowOptions) uninstallflow.Options {
	return uninstallflow.Options{DryRun: opts.DryRun, Yes: opts.Yes, EtcDir: opts.EtcDir, VarDir: opts.VarDir, SystemdDir: opts.SystemdDir, InstallDir: opts.InstallDir}
}

func fromUninstallFlowOptions(opts uninstallflow.Options) uninstallWorkflowOptions {
	return uninstallWorkflowOptions{DryRun: opts.DryRun, Yes: opts.Yes, EtcDir: opts.EtcDir, VarDir: opts.VarDir, SystemdDir: opts.SystemdDir, InstallDir: opts.InstallDir}
}
