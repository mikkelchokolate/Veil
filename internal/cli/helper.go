package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/hostaccess"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/spf13/cobra"
)

type helperCommandDependencies struct {
	GOOS           string
	EffectiveUID   func() int
	LookupUID      func(string) (uint32, error)
	Serve          func(context.Context, string, privileged.PeerPolicy) error
	ServeActivated func(context.Context, privileged.PeerPolicy) error
	// MigrateManagedTree runs the descriptor-pinned hostaccess.Prepare
	// ownership/mode normalization for the packaged layout (#1143).
	MigrateManagedTree func(paths hostaccess.Paths) error
}

func newHelperCommand(version string) *cobra.Command {
	policy := privileged.DefaultPolicy()
	executor := privileged.NewProductionExecutor(privileged.DefaultProductionConfig(policy, version))
	server := privileged.NewServer(privileged.NewLocalAdapter(policy, executor))
	return newHelperCommandWithDependencies(helperCommandDependencies{
		GOOS:               runtime.GOOS,
		EffectiveUID:       os.Geteuid,
		LookupUID:          lookupSystemUID,
		Serve:              server.ServeUnix,
		ServeActivated:     server.ServeSystemd,
		MigrateManagedTree: hostaccess.Prepare,
	})
}

func newHelperCommandWithDependencies(deps helperCommandDependencies) *cobra.Command {
	var socketPath string
	var systemdSocketActivation bool
	var peerUnit string
	helper := &cobra.Command{
		Use:    "helper",
		Short:  "Run Veil privileged helper operations",
		Hidden: true,
	}
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Serve the root helper over a Unix socket",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !systemdSocketActivation && !filepath.IsAbs(socketPath) {
				return fmt.Errorf("helper socket path must be absolute")
			}
			if deps.GOOS == "linux" && deps.EffectiveUID() != 0 {
				return fmt.Errorf("veil helper serve must run as root on Linux")
			}
			uid, err := deps.LookupUID("veil")
			if err != nil {
				return fmt.Errorf("resolve veil user: %w", err)
			}
			// Only the Panel unit may use the helper: uid alone is shared with
			// other software, so peers must also live in the veil.service cgroup
			// (audit #506). --peer-unit can relax it for manual debugging only.
			policy := privileged.PeerPolicy{AllowedUID: uid, AllowRoot: false, AllowedUnit: peerUnit}
			warnHelperPolicyEnvOverrides(cmd.ErrOrStderr())
			if systemdSocketActivation {
				if deps.ServeActivated == nil {
					return fmt.Errorf("systemd socket activation is unavailable")
				}
				return deps.ServeActivated(cmd.Context(), policy)
			}
			return deps.Serve(cmd.Context(), socketPath, policy)
		},
	}
	serve.Flags().StringVar(&socketPath, "socket", privileged.DefaultSocketPath, "absolute Unix socket path")
	serve.Flags().BoolVar(&systemdSocketActivation, "systemd-socket-activation", false, "accept the helper socket from systemd")
	serve.Flags().StringVar(&peerUnit, "peer-unit", "veil.service", "systemd unit a non-root helper peer must belong to (empty disables the unit check)")
	helper.AddCommand(serve)

	// The package postinstall normalizes ownership and modes over trees the
	// veil* service accounts own — including while those daemons are still
	// running mid-upgrade. Doing it in shell with find/chmod/chown walks
	// would race a leaf or directory swapped for a symlink: find decides
	// -type from an lstat and chmod then follows the path, so the mode change
	// could land on an arbitrary root-owned target (#1143). hostaccess.Migrate
	// pins every directory descriptor and applies fchmod/fchown relative to
	// it, refusing symlinks instead of following them — one invocation covers
	// the whole contract the packaging scripts used to hand-roll.
	migrate := &cobra.Command{
		Use:   "migrate",
		Short: "Normalize managed-tree ownership and permissions (packaging hook)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.MigrateManagedTree == nil {
				return fmt.Errorf("managed-tree migration is unavailable")
			}
			if deps.GOOS == "linux" && deps.EffectiveUID() != 0 {
				return fmt.Errorf("veil helper migrate must run as root on Linux")
			}
			return deps.MigrateManagedTree(hostaccess.Paths{EtcDir: defaultEtcDir, VarDir: defaultVarDir})
		},
	}
	helper.AddCommand(migrate)
	return helper
}

// warnHelperPolicyEnvOverrides names every VEIL_* variable that moves the
// root helper's policy paths (state, key, staging/live roots, backups, SFTP
// config) away from the packaged layout. The helper trusts these paths as
// root, and a manual `sudo -E veil helper serve` inherits the operator's
// shell env, so a poisoned env would otherwise retarget key generation,
// backups and restore allowlists without a trace (issue #1263). The packaged
// unit exports the packaged values, which are not reported.
func warnHelperPolicyEnvOverrides(errOut io.Writer) {
	// Values are quoted so an embedded newline cannot forge or split journal
	// lines in the very warning the operator is meant to trust; a redacted
	// placeholder is printed as is.
	for _, override := range privileged.PolicyEnvOverrides() {
		value := strconv.Quote(override.Value)
		if override.Redacted {
			value = override.Value
		}
		fmt.Fprintf(errOut, "warning: %s=%s from the environment overrides the packaged helper path %q; the root helper trusts it\n", override.Name, value, override.Packaged)
	}
}

func lookupSystemUID(name string) (uint32, error) {
	account, err := user.Lookup(name)
	if err != nil {
		return 0, err
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(uid), nil
}
