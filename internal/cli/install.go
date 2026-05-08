package cli

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/spf13/cobra"
	"github.com/veil-panel/veil/internal/installer"
)

var installDNSResolver installer.DNSResolver = installer.NetResolver{}
var installPublicIPClient *http.Client
var installPublicIPEndpoints []string
var installApplyFunc = installer.ApplyRURecommendedProfile

func newInstallCommand() *cobra.Command {
	var profile string
	var stack string
	var domain string
	var email string
	var dryRun bool
	var yes bool
	var etcDir string
	var varDir string
	var systemdDir string
	var panelPort int
	var panelAccess string
	var sharedPort int
	var publicIP string
	var interactive bool
	var hysteriaSHA256 string
	var auditLog string
	var backupDir string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install and configure Veil managed services",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRURecommendedInstall(cmd, ruRecommendedInstallOptions{
				Profile:        profile,
				Stack:          stack,
				Domain:         domain,
				Email:          email,
				DryRun:         dryRun,
				Yes:            yes,
				EtcDir:         etcDir,
				VarDir:         varDir,
				SystemdDir:     systemdDir,
				PanelPort:      panelPort,
				PanelAccess:    panelAccess,
				SharedPort:     sharedPort,
				PublicIP:       publicIP,
				Interactive:    interactive,
				HysteriaSHA256: hysteriaSHA256,
				AuditLog:       auditLog,
				BackupDir:      backupDir,
				BackupDirSet:   cmd.Flags().Changed("backup-dir"),
			})
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "ru-recommended", "install profile: ru-recommended")
	cmd.Flags().StringVar(&stack, "stack", "", "deprecated; Veil install only installs Panel, protocols are configured as Panel Inbounds")
	cmd.Flags().StringVar(&domain, "domain", "", "domain for ACME and client configs")
	cmd.Flags().StringVar(&email, "email", "", "ACME email")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "render installation plan without writing files")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm writing generated files")
	cmd.Flags().StringVar(&etcDir, "etc-dir", "/etc/veil", "Veil configuration directory")
	cmd.Flags().StringVar(&varDir, "var-dir", "/var/lib/veil", "Veil state directory")
	cmd.Flags().StringVar(&systemdDir, "systemd-dir", "", "optional systemd unit output directory, e.g. /etc/systemd/system")
	cmd.Flags().IntVar(&sharedPort, "port", 0, "deprecated; protocols are configured as Panel Inbounds")
	cmd.Flags().IntVar(&panelPort, "panel-port", 0, "panel TCP port; 0 selects a random high port")
	cmd.Flags().StringVar(&panelAccess, "panel-access", "local", "panel access mode: local, direct, or caddy")
	cmd.Flags().StringVar(&publicIP, "public-ip", "", "optional server public IP for DNS validation; use auto to detect it")
	cmd.Flags().StringVar(&hysteriaSHA256, "hysteria-sha256", "", "expected sha256 for the Hysteria2 release asset before binary download")
	cmd.Flags().BoolVar(&interactive, "interactive", false, "prompt for missing ru-recommended install options")
	cmd.Flags().StringVar(&auditLog, "audit-log", "", "optional path for JSONL audit log")
	cmd.Flags().StringVar(&backupDir, "backup-dir", "", "backup directory for files before overwrite (optional; defaults to var-dir/backups; pass empty string to disable)")
	_ = cmd.Flags().MarkHidden("stack")
	_ = cmd.Flags().MarkHidden("port")
	return cmd
}

func writeAuditInstall(auditLog, backupID string, success bool, errMsg string, writtenFiles []string) error {
	return installer.AppendAuditEvent(auditLog, installer.AuditEvent{
		Action:       "install.apply",
		BackupID:     backupID,
		Success:      success,
		Error:        errMsg,
		WrittenFiles: writtenFiles,
	})
}

func randomSecret(label string) string {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return label + "-change-me"
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
