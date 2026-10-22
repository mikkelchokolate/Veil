package cli

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"time"

	doctorflow "github.com/mikkelchokolate/Veil/internal/cliflow/doctor"
	versionflow "github.com/mikkelchokolate/Veil/internal/cliflow/version"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/syspath"
	"github.com/spf13/cobra"
)

var (
	errCommandNotFound = errors.New("command not found")
	// defaultCommandLookPath is the production resolver; TestMain stubs
	// commandLookPath, so #1262 regressions assert on the default directly.
	defaultCommandLookPath = syspath.LookPath
	commandLookPath        = defaultCommandLookPath
	versionCheckClient     = &http.Client{Timeout: 10 * time.Second}
)

func NewRootCommand(version string) *cobra.Command {
	return NewRootCommandWithOptions(version, productionRootOptions())
}

func NewRootCommandWithOptions(version string, options RootOptions) *cobra.Command {
	if options.PasswordHasher == nil {
		options.PasswordHasher = productionRootOptions().PasswordHasher
	}
	cmd := &cobra.Command{
		Use:   "veil",
		Short: "Veil panel and CLI for protocols through Panel Inbounds",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && operatorMenuTerminal(cmd) {
				return runOperatorMenu(cmd, version, options, nil)
			}
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
	cmd.AddCommand(newHealthcheckCommand())
	cmd.AddCommand(newEgressResolvConfCommand())
	cmd.AddCommand(newConfigCommand())
	cmd.AddCommand(newAdminCommand(options.PasswordHasher))
	cmd.AddCommand(newBackupCommand(version))
	cmd.AddCommand(newCertCommand())
	cmd.AddCommand(newHelperCommand(version))
	cmd.AddCommand(newRuntimeCommand())
	cmd.AddCommand(newMenuCommand(version, options))
	attachOperatorHelp(cmd)
	return cmd
}

type doctorSummary = doctorflow.Summary

func printDoctor(cmd *cobra.Command, version string, jsonOutput bool) {
	_ = doctorflow.NewPresentation(cmd.OutOrStdout()).Render(buildDoctorSummary(version), jsonOutput)
}

func buildDoctorSummary(version string) doctorSummary {
	readiness := doctorflow.NewReadiness(version, commandLookPath)
	if runtime.GOOS == "linux" {
		readiness = readiness.WithEgressDNS(doctorEgressDNSWithMieruUnitState)
	}
	return readiness.Summary()
}

// doctorEgressDNSWithMieruUnitState runs the egress DNS probe and asks
// systemd about the loaded veil-mieru.service only when the probe promised
// a Mieru fallback, so a healthy host does not fork `systemctl show` on
// every doctor run.
func doctorEgressDNSWithMieruUnitState() doctorflow.EgressDNSCheck {
	check := doctorEgressDNSProbe()
	if len(check.MieruNameservers) == 0 {
		return check
	}
	return withMieruUnitFallbackState(check, doctorMieruUnitFallbackProbe())
}

// doctorEgressDNSProbe runs the same host probe the Hysteria2 and WARP
// renderers use, so `veil doctor` reports what the next apply renders
// (#1288). Tests replace it.
var doctorEgressDNSProbe doctorflow.EgressDNSProbe = func() doctorflow.EgressDNSCheck {
	return egressDNSCheckFromReport(renderer.ProbeEgressDNS())
}

// egressDNSCheckFromReport maps the renderer probe onto the doctor check.
// Nameservers is never nil, so --json emits [] rather than null for a
// missing or empty resolv.conf.
func egressDNSCheckFromReport(report renderer.EgressDNSReport) doctorflow.EgressDNSCheck {
	nameservers := append([]string{}, report.Nameservers...)
	return doctorflow.EgressDNSCheck{
		Nameservers:       nameservers,
		Reachable:         report.Reachable,
		ResolvConfMissing: report.ResolvConfMissing,
		ReadError:         report.ReadError,
		PinnedDoH:         report.PinnedDoH,
		MieruNameservers:  append([]string(nil), report.MieruNameservers...),
	}
}

// checkLatestVersion fetches the latest Veil release tag from GitHub and
// compares it against the current version. It prints a human-readable
// comparison and returns an error only on network/parse failures.
func checkLatestVersion(cmd *cobra.Command, current string) error {
	return versionflow.NewCheck(current, cmd.OutOrStdout(), fetchLatestReleaseTag).Run()
}

func fetchLatestReleaseTag() (string, error) {
	old := versionflow.HTTPClient
	versionflow.HTTPClient = versionCheckClient
	defer func() { versionflow.HTTPClient = old }()
	return versionflow.FetchLatestReleaseTag()
}
