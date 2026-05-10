package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	updateflow "github.com/veil-panel/veil/internal/cliflow/update"
	"github.com/veil-panel/veil/internal/renderer"
)

var updateHealthChecker = updateflow.WaitForHealthy

func restartUpdatedVeil(cmd *cobra.Command, currentPath string, backupPath string, opts updateWorkflowOptions) error {
	out := cmd.OutOrStdout()
	addr := resolveStatusListen(opts.Listen)
	token, _ := resolveServeAuthToken(opts.AuthToken)

	fmt.Fprintln(out, "Restarting "+renderer.UnitVeil+"...")
	if err := runSystemctlRestart(renderer.UnitVeil); err != nil {
		if opts.Staged {
			fmt.Fprintf(out, "Restart failed, rolling back to previous binary...\n")
			if rollbackErr := updateflow.RollbackBinary(backupPath, currentPath); rollbackErr != nil {
				return fmt.Errorf("restart failed and rollback also failed: restart: %w; rollback: %v", err, rollbackErr)
			}
			fmt.Fprintln(out, "Rolled back to previous binary.")
			return fmt.Errorf("restart failed, rolled back: %w", err)
		}
		return fmt.Errorf("restart failed (binary updated, rollback with: mv %s %s): %w", backupPath, currentPath, err)
	}
	fmt.Fprintln(out, "Service restarted. Running health check...")

	if err := updateHealthChecker(addr, token, 10*time.Second); err != nil {
		if opts.Staged {
			fmt.Fprintf(out, "Health check failed, rolling back to previous binary...\n")
			if rollbackErr := updateflow.RollbackBinary(backupPath, currentPath); rollbackErr != nil {
				return fmt.Errorf("health check failed and rollback also failed: health: %w; rollback: %v", err, rollbackErr)
			}
			// Restart again after rollback to get the old binary running
			if restartErr := runSystemctlRestart(renderer.UnitVeil); restartErr != nil {
				fmt.Fprintf(out, "Warning: rollback binary installed but restart failed: %v\n", restartErr)
			}
			fmt.Fprintln(out, "Rolled back to previous binary.")
			return fmt.Errorf("health check failed, rolled back: %w", err)
		}
		return fmt.Errorf("health check failed after restart (binary updated, rollback with: mv %s %s): %w", backupPath, currentPath, err)
	}
	fmt.Fprintf(out, "Service healthy. Update complete.\n")
	return nil
}
