package cli

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/spf13/cobra"
)

type helperCommandDependencies struct {
	GOOS           string
	EffectiveUID   func() int
	LookupUID      func(string) (uint32, error)
	Serve          func(context.Context, string, uint32, bool) error
	ServeActivated func(context.Context, uint32, bool) error
}

func newHelperCommand(version string) *cobra.Command {
	policy := privileged.DefaultPolicy()
	executor := privileged.NewProductionExecutor(privileged.DefaultProductionConfig(policy, version))
	server := privileged.NewServer(privileged.NewLocalAdapter(policy, executor))
	return newHelperCommandWithDependencies(helperCommandDependencies{
		GOOS:           runtime.GOOS,
		EffectiveUID:   os.Geteuid,
		LookupUID:      lookupSystemUID,
		Serve:          server.ServeUnix,
		ServeActivated: server.ServeSystemd,
	})
}

func newHelperCommandWithDependencies(deps helperCommandDependencies) *cobra.Command {
	var socketPath string
	var systemdSocketActivation bool
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
			if systemdSocketActivation {
				if deps.ServeActivated == nil {
					return fmt.Errorf("systemd socket activation is unavailable")
				}
				return deps.ServeActivated(cmd.Context(), uid, false)
			}
			return deps.Serve(cmd.Context(), socketPath, uid, false)
		},
	}
	serve.Flags().StringVar(&socketPath, "socket", privileged.DefaultSocketPath, "absolute Unix socket path")
	serve.Flags().BoolVar(&systemdSocketActivation, "systemd-socket-activation", false, "accept the helper socket from systemd")
	helper.AddCommand(serve)
	return helper
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
