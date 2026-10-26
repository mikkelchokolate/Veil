package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	doctorflow "github.com/mikkelchokolate/Veil/internal/cliflow/doctor"
	repairflow "github.com/mikkelchokolate/Veil/internal/cliflow/repair"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/veilenv"
	"github.com/spf13/cobra"
)

type repairWorkflowOptions struct {
	Profile         string
	DryRun          bool
	Yes             bool
	EtcDir          string
	VarDir          string
	SystemdDir      string
	BackupDir       string
	BackupDirSet    bool
	AuditLog        string
	LEIPCert        bool
	LEIPCertSet     bool
	LEIPCertPort    int
	LEIPCertPortSet bool
	PublicIP        string
}

func newRepairCommand() *cobra.Command {
	var profile string
	var dryRun bool
	var yes bool
	var etcDir string
	var varDir string
	var systemdDir string
	var backupDir string
	var auditLog string
	var leIPCert bool
	var leIPCertPort int
	var publicIP string

	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Repair Veil managed generated files without arbitrary side effects",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Repair has to work on the tree the host is actually installed
			// with. On a custom --etc-dir/--var-dir install the packaged
			// defaults would rewrite the wrong veil.env and leave the custom
			// units stale (#1473), so the installed layout is discovered first.
			resolvedEtc, resolvedVar, err := resolveRepairLayout(cmd, etcDir, varDir)
			if err != nil {
				return err
			}
			etcDir, varDir = resolvedEtc, resolvedVar
			// Same contract for the unit directory: a custom --systemd-dir
			// install records it in veil.env, and re-rendering the units into
			// the packaged path would leave the live ones stale (#1484).
			systemdDir = resolveRepairSystemdDir(cmd, etcDir, systemdDir)
			return runRepairWorkflow(cmd, repairWorkflowOptions{
				Profile:         profile,
				DryRun:          dryRun,
				Yes:             yes,
				EtcDir:          etcDir,
				VarDir:          varDir,
				SystemdDir:      systemdDir,
				BackupDir:       backupDir,
				BackupDirSet:    cmd.Flags().Changed("backup-dir"),
				AuditLog:        auditLog,
				LEIPCert:        leIPCert,
				LEIPCertSet:     cmd.Flags().Changed("le-ip-cert"),
				LEIPCertPort:    leIPCertPort,
				LEIPCertPortSet: cmd.Flags().Changed("le-ip-cert-port"),
				PublicIP:        publicIP,
			})
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "ru-recommended", "repair profile: ru-recommended")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print repair plan without writing files")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm repairing planned files")
	cmd.Flags().StringVar(&etcDir, "etc-dir", "/etc/veil", "Veil configuration directory")
	cmd.Flags().StringVar(&varDir, "var-dir", "/var/lib/veil", "Veil state directory")
	cmd.Flags().StringVar(&systemdDir, "systemd-dir", defaultSystemdDir, "systemd unit output directory")
	cmd.Flags().StringVar(&backupDir, "backup-dir", "", "backup directory for files before overwrite (optional; defaults to var-dir/backups; pass empty string to disable)")
	cmd.Flags().StringVar(&auditLog, "audit-log", "", "optional path for JSONL audit log")
	cmd.Flags().BoolVar(&leIPCert, "le-ip-cert", true, "re-issue Let's Encrypt IP certificate in direct mode if needed")
	cmd.Flags().IntVar(&leIPCertPort, "le-ip-cert-port", 80, "port used for Let's Encrypt HTTP-01 validation")
	cmd.Flags().StringVar(&publicIP, "public-ip", "", "optional server public IP; use auto to detect it")
	return cmd
}

// resolveRepairSystemdDir picks the unit directory repair writes into: an
// explicit --systemd-dir wins, then the directory the installed veil.env
// records, then the packaged default.
func resolveRepairSystemdDir(cmd *cobra.Command, etcDir, flagValue string) string {
	if cmd.Flags().Changed("systemd-dir") {
		return flagValue
	}
	if dir := installedSystemdDir(filepath.Join(etcDir, "veil.env")); dir != "" {
		return dir
	}
	if strings.TrimSpace(flagValue) == "" {
		return defaultSystemdDir
	}
	return flagValue
}

// repairDiscoverySystemdDir is where the installed veil.service drop-in that
// records a custom EnvironmentFile= is discovered. Tests replace it.
var repairDiscoverySystemdDir = veilenv.DefaultSystemdDir

// installedLayout is the configuration tree a Veil install uses.
type installedLayout struct {
	etcDir string
	varDir string
}

// discoverInstalledLayout reads the installed environment file (the same
// discovery order rotate-key and status use) and returns the layout it
// describes. ok is false when there is no installed file or it does not name a
// complete custom tree, in which case the packaged defaults still apply.
func discoverInstalledLayout() (installedLayout, bool) {
	envPath := veilenv.Resolve(repairDiscoverySystemdDir)
	body, err := os.ReadFile(envPath)
	if err != nil {
		return installedLayout{}, false
	}
	etcDir := veilenv.Value(body, "VEIL_ETC_DIR")
	if etcDir == "" {
		if keyPath := veilenv.Value(body, veilenv.KeyPathKey); keyPath != "" {
			etcDir = filepath.Dir(keyPath)
		} else if envPath != veilenv.DefaultPath() {
			etcDir = filepath.Dir(envPath)
		}
	}
	varDir := veilenv.Value(body, "VEIL_VAR_DIR")
	if varDir == "" {
		if statePath := veilenv.Value(body, "VEIL_STATE_PATH"); statePath != "" {
			varDir = filepath.Dir(statePath)
		}
	}
	if etcDir == "" && varDir == "" {
		return installedLayout{}, false
	}
	// A layout is allowed to record only one dimension: `install` takes --etc-dir
	// and --var-dir independently, and a repaired env may only carry the etc
	// block. Unknown dimensions are compared against the packaged default.
	return installedLayout{etcDir: etcDir, varDir: varDir}, true
}

// resolveRepairLayout picks the tree repair works on. Explicit flags win, then
// an explicit process environment (VEIL_ETC_DIR/VEIL_VAR_DIR, which hostenv
// already honours), and finally the packaged defaults. When the discovered
// layout differs from those defaults, repair refuses rather than silently
// fixing the wrong tree.
func resolveRepairLayout(cmd *cobra.Command, etcFlag, varFlag string) (string, string, error) {
	if cmd.Flags().Changed("etc-dir") || cmd.Flags().Changed("var-dir") {
		return etcFlag, varFlag, nil
	}
	if strings.TrimSpace(os.Getenv("VEIL_ETC_DIR")) != "" || strings.TrimSpace(os.Getenv("VEIL_VAR_DIR")) != "" {
		return hostenv.EtcDir(), hostenv.VarDir(), nil
	}
	installed, ok := discoverInstalledLayout()
	if !ok {
		return etcFlag, varFlag, nil
	}
	if installed.etcDir == "" {
		installed.etcDir = etcFlag
	}
	if installed.varDir == "" {
		installed.varDir = varFlag
	}
	if sameKeyPath(installed.etcDir, etcFlag) && sameKeyPath(installed.varDir, varFlag) {
		return etcFlag, varFlag, nil
	}
	return "", "", fmt.Errorf("this host is installed with a custom layout (%s, %s); rerun with --etc-dir %s --var-dir %s so repair fixes the installed tree instead of writing the packaged defaults", installed.etcDir, installed.varDir, installed.etcDir, installed.varDir)
}

// repairBuildPlan builds the repair plan. Tests replace it to exercise the
// warning wiring without host stubs.
var repairBuildPlan = repairflow.BuildPlanFromOptions

func runRepairWorkflow(cmd *cobra.Command, opts repairWorkflowOptions) error {
	var planWarnings []string
	flowOpts := repairflow.Options{Profile: opts.Profile, DryRun: opts.DryRun, Yes: opts.Yes, EtcDir: opts.EtcDir, VarDir: opts.VarDir, SystemdDir: opts.SystemdDir, BackupDir: opts.BackupDir, BackupDirSet: opts.BackupDirSet, AuditLog: opts.AuditLog, LEIPCert: opts.LEIPCert, LEIPCertSet: opts.LEIPCertSet, LEIPCertPort: opts.LEIPCertPort, LEIPCertPortSet: opts.LEIPCertPortSet, PublicIP: opts.PublicIP}
	return repairflow.Run(flowOpts, cmd.OutOrStdout(), repairflow.Dependencies{
		BuildPlan: func(flowOpts repairflow.Options) (installer.RepairPlan, error) {
			// Collect the non-fatal findings of this build and print them with
			// the plan: a failed certificate renewal or an unreproducible Caddy
			// configuration must not hide behind a successful exit (#1500).
			planWarnings = nil
			return repairBuildPlan(flowOpts, repairflow.PlanDependencies{
				Secret: randomSecret, Executable: installExecutableFunc, LookPath: commandLookPath,
				Notify: func(msg string) { planWarnings = append(planWarnings, msg) },
			})
		},
		ApplyPlan: func(plan installer.RepairPlan, flowOpts repairflow.Options) error {
			return repairflow.ApplyPlan(plan, flowOpts, cmd.OutOrStdout(), repairflow.ApplyDependencies{RunSystemd: installSystemdRunFunc})
		},
		EgressDNSWarning: repairEgressDNSWarning,
		Warnings:         func() []string { return planWarnings },
	})
}

// repairEgressDNSWarning reports the protocol-unit DNS warning `veil doctor`
// prints (#1288). The egress filter only exists on Linux; the platform seam
// is the one install's preflight uses, so one stub covers both commands.
func repairEgressDNSWarning() string {
	if installPlatformFunc().OS != "linux" {
		return ""
	}
	return doctorflow.EgressDNSWarning(doctorEgressDNSProbe())
}
