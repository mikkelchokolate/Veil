package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
	"github.com/spf13/cobra"
)

// runtimeInstallFunc is injectable so tests can exercise the command without
// reaching the network.
var runtimeInstallFunc = func(ctx context.Context, opts runtimeinstall.Options, only []string) ([]runtimeinstall.Result, error) {
	return protocols.InstallSelectedRuntimes(ctx, opts, only)
}

var runtimeEnsureFunc = runtimeinstall.Ensure

// installRuntimesFunc installs Caddy when it is needed to expose the panel.
// Protocol runtimes are acquired when their first enabled inbound is applied.
var installRuntimesFunc = defaultInstallRuntimesDuringInstall

func defaultInstallRuntimesDuringInstall(cmd *cobra.Command, opts ruRecommendedInstallOptions) error {
	arch, err := hostenv.NormalizeArch(hostenv.CurrentPlatform().Arch)
	if err != nil {
		return fmt.Errorf("protocol runtime install: %w", err)
	}
	plugin, _ := protocols.NewRegistry().Get("naiveproxy")
	provider, ok := protocols.AsRuntimeProvider(plugin)
	if !ok {
		return fmt.Errorf("panel Caddy runtime is unavailable")
	}
	fmt.Fprintln(cmd.OutOrStdout())
	result := runtimeEnsureFunc(cmd.Context(), runtimeinstall.Options{BinDir: runtimeinstall.DefaultBinDir(), Arch: arch}, provider.RuntimeInstall(arch))
	if result.Err == nil && !result.Installed {
		result.Err = fmt.Errorf("Caddy runtime was not installed: %s", result.SkipReason)
	}
	if result.Err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "You can retry with: sudo veil runtime install --only naiveproxy")
		return fmt.Errorf("install panel Caddy runtime: %w", result.Err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Panel Caddy runtime verified: %s\n", result.Path)
	return nil
}

// runtimeNames returns the sorted list of protocol runtime names from the
// registry and the runtimeinstall catalog. It is used to build dynamic help
// text so the CLI never lists stale or missing protocol names.
func runtimeNames() []string {
	names := make(map[string]struct{})

	// Collect from protocol plugins that provide runtimes.
	for _, p := range protocols.NewRegistry().All() {
		if rp, ok := protocols.AsRuntimeProvider(p); ok {
			rt := rp.RuntimeInstall("")
			if rt.Name != "" {
				names[rt.Name] = struct{}{}
			}
		}
	}

	// Collect from the runtimeinstall catalog (covers WARP and any
	// non-plugin-managed runtimes).
	for _, rt := range runtimeinstall.Catalog("") {
		if rt.Name != "" {
			names[rt.Name] = struct{}{}
		}
	}

	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// runtimeDescription returns the human-readable acquisition description for a
// registered runtime, if one is provided by the plugin or catalog.
func runtimeDescription(name string) string {
	for _, p := range protocols.NewRegistry().All() {
		if rp, ok := protocols.AsRuntimeProvider(p); ok {
			rt := rp.RuntimeInstall("")
			if rt.Name == name {
				return rt.Description
			}
		}
	}
	for _, rt := range runtimeinstall.Catalog("") {
		if rt.Name == name {
			return rt.Description
		}
	}
	return ""
}

// binaryNames returns the sorted list of binary filenames managed by the
// runtime install system. Used for the Short help text.
func binaryNames() []string {
	names := make(map[string]struct{})

	for _, p := range protocols.NewRegistry().All() {
		if rp, ok := protocols.AsRuntimeProvider(p); ok {
			rt := rp.RuntimeInstall("")
			if rt.Binary != "" {
				names[rt.Binary] = struct{}{}
			}
		}
	}

	for _, rt := range runtimeinstall.Catalog("") {
		if rt.Binary != "" {
			names[rt.Binary] = struct{}{}
		}
	}

	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func newRuntimeCommand() *cobra.Command {
	binaries := binaryNames()
	short := "Manage protocol runtime binaries"
	if len(binaries) > 0 {
		short = fmt.Sprintf("Manage protocol runtime binaries (%s)", strings.Join(binaries, ", "))
	}

	cmd := &cobra.Command{
		Use:   "runtime",
		Short: short,
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

	names := runtimeNames()
	onlyDesc := "install only these runtimes (by protocol name)"
	if len(names) > 0 {
		onlyDesc = fmt.Sprintf("install only these runtimes (by protocol name: %s)", strings.Join(names, ", "))
	}

	// Build the Long description dynamically so it always matches the
	// current set of registered runtimes.
	var longParts []string
	for _, name := range names {
		desc := runtimeDescription(name)
		if desc == "" {
			desc = fmt.Sprintf("%s runtime", name)
		}
		longParts = append(longParts, desc)
	}

	longBody := "Preinstall or repair the external runtime binaries that Veil's managed\nsystemd units invoke (default /usr/local/bin). Runtimes are normally acquired\non demand when their first enabled inbound is applied or WARP is enabled.\nUse --only to prepare selected runtimes in advance."
	if len(longParts) > 0 {
		longBody += "\n\n" + strings.Join(longParts, "; ") + "."
	}
	longBody += "\n\nUse --only to install a subset, e.g. --only mieru,hysteria2."

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Download and install protocol runtime binaries into the bin directory",
		Long:  longBody,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("only") && len(only) == 0 {
				return fmt.Errorf("--only requires at least one runtime name")
			}
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
	cmd.Flags().StringSliceVar(&only, "only", nil, onlyDesc)
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
	if err := rejectUnknownRuntimeSelection(opts.Only); err != nil {
		return err
	}
	results, err := runtimeInstallFunc(ctx, runtimeinstall.Options{BinDir: opts.BinDir, Arch: opts.Arch}, opts.Only)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		if len(opts.Only) > 0 {
			return fmt.Errorf("no matching runtimes for --only %s", strings.Join(opts.Only, ","))
		}
		return fmt.Errorf("no protocol runtimes are available for this platform")
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

func rejectUnknownRuntimeSelection(only []string) error {
	if len(only) == 0 {
		return nil
	}
	if len(runtimeinstall.NormalizeRuntimeNames(only)) == 0 {
		return fmt.Errorf("--only requires at least one runtime name")
	}
	catalog := make([]runtimeinstall.Runtime, 0, len(runtimeNames()))
	for _, name := range runtimeNames() {
		catalog = append(catalog, runtimeinstall.Runtime{Name: name})
	}
	_, err := runtimeinstall.FilterCatalog(catalog, only)
	return err
}
