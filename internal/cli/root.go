package cli

import (
	"encoding/json"
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
	cmd.AddCommand(newServeCommand(version))
	return cmd
}

type doctorSummary struct {
	Version  string                `json:"version"`
	Runtime  string                `json:"runtime"`
	Commands []doctorCommandStatus `json:"commands"`
}

type doctorCommandStatus struct {
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Present bool   `json:"present"`
}

func printDoctor(cmd *cobra.Command, version string, jsonOutput bool) {
	out := cmd.OutOrStdout()
	summary := buildDoctorSummary(version)
	if jsonOutput {
		_ = json.NewEncoder(out).Encode(summary)
		return
	}
	fmt.Fprintln(out, "Veil doctor")
	fmt.Fprintf(out, "Version: %s\n", summary.Version)
	fmt.Fprintf(out, "Runtime: %s\n", summary.Runtime)
	fmt.Fprintln(out, "Required commands:")
	for _, command := range summary.Commands {
		if !command.Present {
			fmt.Fprintf(out, "- %s: missing\n", command.Name)
			continue
		}
		fmt.Fprintf(out, "- %s: %s\n", command.Name, command.Path)
	}
}

func buildDoctorSummary(version string) doctorSummary {
	summary := doctorSummary{
		Version: version,
		Runtime: runtime.GOOS + "/" + runtime.GOARCH,
	}
	for _, name := range []string{"caddy", "hysteria", "systemctl"} {
		status := doctorCommandStatus{Name: name}
		path, err := exec.LookPath(name)
		if err == nil {
			status.Path = path
			status.Present = true
		}
		summary.Commands = append(summary.Commands, status)
	}
	return summary
}
