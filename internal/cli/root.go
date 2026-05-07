package cli

import (
	"errors"
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
)

var (
	errCommandNotFound = errors.New("command not found")
	commandLookPath    = exec.LookPath
)

func NewRootCommand(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "veil",
		Short: "Veil panel and CLI for NaiveProxy + Hysteria2",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	var checkUpdate bool
	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print Veil version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version)
			if checkUpdate {
				return checkLatestVersion(cmd, version)
			}
			return nil
		},
	}
	versionCmd.Flags().BoolVar(&checkUpdate, "check", false, "check for newer Veil releases on GitHub")
	cmd.AddCommand(versionCmd)
	var doctorJSON bool
	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run host readiness checks",
		Run: func(cmd *cobra.Command, args []string) {
			printDoctor(cmd, version, doctorJSON)
		},
	}
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "print doctor readiness summary as JSON")
	cmd.AddCommand(doctorCmd)
	cmd.AddCommand(newInstallCommand())
	cmd.AddCommand(newRepairCommand())
	cmd.AddCommand(newUninstallCommand())
	cmd.AddCommand(newRollbackCommand())
	cmd.AddCommand(newServeCommand(version))
	cmd.AddCommand(newUpdateCommand(version))
	cmd.AddCommand(newStatusCommand(version))
	cmd.AddCommand(newConfigCommand())
	return cmd
}

type doctorSummary struct {
	Version  string                `json:"version"`
	Runtime  string                `json:"runtime"`
	Ready    bool                  `json:"ready"`
	Commands []doctorCommandStatus `json:"commands"`
}

type doctorCommandStatus struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Error    string `json:"error,omitempty"`
	Present  bool   `json:"present"`
	Optional bool   `json:"optional,omitempty"`
}

func printDoctor(cmd *cobra.Command, version string, jsonOutput bool) {
	_ = NewDoctorPresentation(cmd.OutOrStdout()).Render(buildDoctorSummary(version), jsonOutput)
}

// checkLatestVersion fetches the latest Veil release tag from GitHub and
// compares it against the current version. It prints a human-readable
// comparison and returns an error only on network/parse failures.
func checkLatestVersion(cmd *cobra.Command, current string) error {
	return NewVersionCheck(current, cmd.OutOrStdout()).Run()
}
