package cli

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

func NewRootCommand(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "veil",
		Short: "Veil panel and CLI for NaiveProxy + Hysteria2",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print Veil version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Run host readiness checks",
		Run: func(cmd *cobra.Command, args []string) {
			printDoctor(cmd, version)
		},
	})
	cmd.AddCommand(newInstallCommand())
	cmd.AddCommand(newRepairCommand())
	cmd.AddCommand(newServeCommand(version))
	return cmd
}

func printDoctor(cmd *cobra.Command, version string) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Veil doctor")
	fmt.Fprintf(out, "Version: %s\n", version)
	fmt.Fprintf(out, "Runtime: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintln(out, "Required commands:")
	for _, name := range []string{"caddy", "hysteria", "systemctl"} {
		path, err := exec.LookPath(name)
		if err != nil {
			fmt.Fprintf(out, "- %s: missing\n", name)
			continue
		}
		fmt.Fprintf(out, "- %s: %s\n", name, path)
	}
}
