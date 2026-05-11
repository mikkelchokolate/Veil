package cli

import (
	"net/http"
	"time"

	"github.com/spf13/cobra"
	updateflow "github.com/veil-panel/veil/internal/cliflow/update"
)

const (
	updateRepoOwner = "mikkelchokolate"
	updateRepoName  = "Veil"
	updateTimeout   = 5 * time.Minute
)

var updateHTTPClient = &http.Client{Timeout: 30 * time.Second}

func newUpdateCommand(version string) *cobra.Command {
	var yes bool
	var dryRun bool
	var force bool
	var restart bool
	var staged bool
	var listen string
	var authToken string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Download and install the latest Veil release",
		Long: `Update downloads the latest Veil release from GitHub, verifies its SHA256
checksum, backs up the current binary, and replaces it.

Use --dry-run to preview without making changes.
Use --force to reinstall even if the current version is already the latest.
Use --restart to restart the veil service and perform a health check after update.
Use --staged for a safer staged rollout: restart + health check with automatic
rollback to the previous binary if the health check fails.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return updateflow.RunWorkflow(updateflow.WorkflowOptions{CurrentVersion: version, Yes: yes, DryRun: dryRun, Force: force, Restart: restart, Staged: staged, Listen: listen, AuthToken: authToken}, cmd.OutOrStdout(), updateflow.WorkflowDependencies{FetchRelease: fetchLatestRelease, DownloadAsset: downloadAsset, RestartUpdated: func(currentPath string, backupPath string, opts updateflow.WorkflowOptions) error {
				return restartUpdatedVeil(cmd, currentPath, backupPath, opts)
			}})
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "confirm binary replacement")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview update without making changes")
	cmd.Flags().BoolVar(&force, "force", false, "reinstall even if already at latest version")
	cmd.Flags().BoolVar(&restart, "restart", false, "restart veil.service and health check after update")
	cmd.Flags().BoolVar(&staged, "staged", false, "restart with health check and automatic rollback on failure")
	cmd.Flags().StringVar(&listen, "listen", "", "veil serve address for health check (default: 127.0.0.1:2096)")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "API token for health check")
	return cmd
}

func fetchLatestRelease() (*updateflow.Release, error) {
	catalog := updateflow.NewReleaseCatalog(updateRepoOwner, updateRepoName)
	catalog.HTTPClient = updateHTTPClient
	return catalog.Latest()
}

func downloadAsset(url string) ([]byte, error) {
	updateflow.HTTPClient = updateHTTPClient
	return updateflow.DownloadAsset(url)
}

// runSystemctlRestart runs systemctl restart for the given unit.
var runSystemctlRestart = updateflow.RunSystemctlRestart
