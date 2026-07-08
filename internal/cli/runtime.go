package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/spf13/cobra"
)

// runtimeInstallFunc is injectable so tests can exercise the command without
// reaching the network.
var runtimeInstallFunc = protocols.InstallSelectedRuntimes

// installRuntimesFunc provisions protocol runtimes during `veil install`. It is
// intentionally non-fatal: a fresh Panel install must still succeed even if a
// single upstream release download fails, so failures are reported as warnings
// and the operator can re-run `veil runtime install` later.
var installRuntimesFunc = defaultInstallRuntimesDuringInstall

func defaultInstallRuntimesDuringInstall(cmd *cobra.Command, opts ruRecommendedInstallOptions) {
	arch, err := hostenv.NormalizeArch(hostenv.CurrentPlatform().Arch)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipping runtime install: %v\n", err)
		return
	}
	fmt.Fprintln(cmd.OutOrStdout())
	if err := runRuntimeInstall(cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.Context(), runtimeInstallOptions{Arch: arch}); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: some protocol runtimes could not be installed: %v\n", err)
		fmt.Fprintln(cmd.ErrOrStderr(), "You can retry later with: sudo veil runtime install")
	}
}

func newRuntimeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runtime",
		Short: "Manage protocol runtime binaries (caddy, hysteria, mita, sing-box, olcrtc)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newRuntimeInstallCommand())
	return cmd
}

func newRuntimeInstallCommand() *cobra.Command {
	var binDir string
	var only []string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Download and install protocol runtime binaries into the bin directory",
		Long: `Install acquires the external runtime binaries that Veil's managed systemd
units invoke and places them in the bin directory (default /usr/local/bin).

Without these binaries every protocol fails to start with systemd status
203/EXEC. caddy, hysteria, mita, and sing-box are downloaded from their upstream
GitHub releases (with checksum verification where published); olcrtc is built
from source with "go install".

Use --only to install a subset, e.g. --only mieru,hysteria2.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			arch, err := hostenv.NormalizeArch(hostenv.CurrentPlatform().Arch)
			if err != nil {
				return err
			}
			return runRuntimeInstall(cmd.OutOrStdout(), cmd.ErrOrStderr(), cmd.Context(), runtimeInstallOptions{
				BinDir: binDir,
				Arch:   arch,
				Only:   only,
			})
		},
	}
	cmd.Flags().StringVar(&binDir, "bin-dir", runtimeinstall.DefaultBinDir(), "directory to install runtime binaries into")
	cmd.Flags().StringSliceVar(&only, "only", nil, "install only these runtimes (by protocol name: naiveproxy, hysteria2, mieru, warp, olcrtc)")
	return cmd
}

type runtimeInstallOptions struct {
	BinDir string
	Arch   string
	Only   []string
}

func runRuntimeInstall(out io.Writer, errOut io.Writer, ctx context.Context, opts runtimeInstallOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	results := runtimeInstallFunc(ctx, runtimeinstall.Options{BinDir: opts.BinDir, Arch: opts.Arch}, opts.Only)
	if len(results) == 0 {
		return fmt.Errorf("no matching runtimes for --only %s", strings.Join(opts.Only, ","))
	}

	fmt.Fprintln(out, "Installing protocol runtimes")
	fmt.Fprintln(out, strings.Repeat("-", 28))
	var failures []string
	for _, result := range results {
		switch {
		case result.Err != nil:
			failures = append(failures, result.Name)
			fmt.Fprintf(errOut, "- %s (%s): failed: %v\n", result.Name, result.Binary, result.Err)
		case result.Installed:
			version := result.Version
			if version == "" {
				version = "from source"
			}
			fmt.Fprintf(out, "- %s (%s): installed %s -> %s\n", result.Name, result.Binary, version, result.Path)
		default:
			fmt.Fprintf(out, "- %s (%s): skipped (%s)\n", result.Name, result.Binary, result.SkipReason)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("failed to install runtimes: %s", strings.Join(failures, ", "))
	}
	fmt.Fprintln(out, "All requested protocol runtimes are installed.")
	return nil
}
