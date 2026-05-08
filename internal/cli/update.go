package cli

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
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
			return runUpdateWorkflow(cmd, updateWorkflowOptions{
				CurrentVersion: version,
				Yes:            yes,
				DryRun:         dryRun,
				Force:          force,
				Restart:        restart,
				Staged:         staged,
				Listen:         listen,
				AuthToken:      authToken,
			})
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

// runSystemctlRestart runs systemctl restart for the given unit.
var runSystemctlRestart = func(unit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return execCommand(ctx, "systemctl", "restart", unit)
}

var execCommand = func(ctx context.Context, name string, args ...string) error {
	cmd := execCmd(ctx, name, args...)
	return cmd.Run()
}

var execCmd = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// waitForHealthy polls the /healthz endpoint until it returns 200 or times out.
func waitForHealthy(addr string, token string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	candidates := statusCandidateAddrs(addr)
	for time.Now().Before(deadline) {
		for _, candidate := range candidates {
			url := candidate + "/healthz"
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				cancel()
				continue
			}
			if token != "" {
				req.Header.Set("X-Veil-Token", token)
			}
			resp, err := statusHTTPClient(url).Do(req)
			cancel()
			if err == nil && resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				return nil
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("health check timed out after %v", timeout)
}
