package update

import (
	"fmt"
	"io"
	"time"

	statusflow "github.com/mikkelchokolate/Veil/internal/cliflow/status"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

type RestartHooks struct {
	Restart  func(unit string) error
	Health   func(addr, token string, timeout time.Duration) error
	Rollback func(backupPath, currentPath string) error
}

func (h RestartHooks) withDefaults() RestartHooks {
	if h.Restart == nil {
		h.Restart = RunSystemctlRestart
	}
	if h.Health == nil {
		h.Health = WaitForHealthy
	}
	if h.Rollback == nil {
		h.Rollback = RollbackBinary
	}
	return h
}

func RestartAfterUpdate(out io.Writer, currentPath, backupPath string, opts WorkflowOptions, hooks RestartHooks) error {
	hooks = hooks.withDefaults()
	addr := statusflow.ResolveListen(opts.Listen)
	token := opts.AuthToken

	fmt.Fprintln(out, "Restarting "+renderer.UnitVeil+"...")
	if err := hooks.Restart(renderer.UnitVeil); err != nil {
		if opts.Staged {
			return rollbackStagedUpdate(out, currentPath, backupPath, addr, token, "restart", err, hooks)
		}
		return fmt.Errorf("restart failed (binary updated, rollback with: mv %s %s): %w", backupPath, currentPath, err)
	}
	fmt.Fprintln(out, "Service restarted. Running health check...")

	if err := hooks.Health(addr, token, 10*time.Second); err != nil {
		if opts.Staged {
			return rollbackStagedUpdate(out, currentPath, backupPath, addr, token, "health check", err, hooks)
		}
		return fmt.Errorf("health check failed after restart (binary updated, rollback with: mv %s %s): %w", backupPath, currentPath, err)
	}
	fmt.Fprintf(out, "Service healthy. Update complete.\n")
	return nil
}

func rollbackStagedUpdate(out io.Writer, currentPath, backupPath, addr, token, kind string, causeErr error, hooks RestartHooks) error {
	label := "Restart failed"
	causeKey := "restart"
	if kind == "health check" {
		label = "Health check failed"
		causeKey = "health"
	}
	fmt.Fprintf(out, "%s, rolling back to previous binary...\n", label)
	if rollbackErr := hooks.Rollback(backupPath, currentPath); rollbackErr != nil {
		return fmt.Errorf("%s failed and rollback also failed: %s: %w; rollback: %v", kind, causeKey, causeErr, rollbackErr)
	}
	if restartErr := hooks.Restart(renderer.UnitVeil); restartErr != nil {
		return fmt.Errorf("%s failed: %w; restored previous binary but restart failed: %v", kind, causeErr, restartErr)
	}
	if healthErr := hooks.Health(addr, token, 10*time.Second); healthErr != nil {
		return fmt.Errorf("%s failed: %w; restored previous binary but health check failed: %v", kind, causeErr, healthErr)
	}
	fmt.Fprintln(out, "Rolled back to previous binary.")
	return fmt.Errorf("%s failed, rolled back: %w", kind, causeErr)
}
