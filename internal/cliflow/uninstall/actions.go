package uninstall

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type CommandRunner interface {
	Run(veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput
}

type Actions struct {
	runner      CommandRunner
	fileRemover func(string) error
}

func NewActions(runner CommandRunner, fileRemover func(string) error) Actions {
	if runner == nil {
		runner = veilruntime.NewRuntimeCommandExecutor()
	}
	if fileRemover == nil {
		fileRemover = os.RemoveAll
	}
	return Actions{runner: runner, fileRemover: fileRemover}
}

// defaultActions constructs the Actions used by package-level helpers.
// It is a variable so tests can substitute a mock and avoid invoking real systemctl.
var defaultActions = func() Actions {
	return NewActions(nil, nil)
}

func DefaultDependencies() Dependencies {
	actions := defaultActions()
	return Dependencies{ServiceStopper: actions.StopAndDisableService, FileRemover: actions.RemovePath, SystemdReloader: actions.ReloadSystemdDaemon}
}

func StopAndDisableService(service string) error {
	return defaultActions().StopAndDisableService(service)
}

func RemovePath(path string) error {
	return defaultActions().RemovePath(path)
}

func ReloadSystemdDaemon() error {
	return defaultActions().ReloadSystemdDaemon()
}

func (a Actions) StopAndDisableService(service string) error {
	stopTarget := service
	if glob := instanceStopGlob(service); glob != "" {
		// Template units such as veil-hysteria2@.service cannot be stopped
		// themselves. Stop loaded instances via systemd's @* glob first.
		stopTarget = glob
	}
	if err := a.run("stop", []string{"systemctl", "stop", stopTarget}); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	if err := a.run("disable", []string{"systemctl", "disable", service}); err != nil {
		return fmt.Errorf("disable: %w", err)
	}
	return nil
}

func instanceStopGlob(service string) string {
	const suffix = "@.service"
	if strings.HasSuffix(service, suffix) {
		return strings.TrimSuffix(service, suffix) + "@*"
	}
	return ""
}

func (a Actions) RemovePath(path string) error {
	if err := ensureRemovablePath(path); err != nil {
		return err
	}
	return a.fileRemover(path)
}

// stateDirMarkers are files and directories whose presence proves a directory
// belongs to Veil (or to a Veil-managed runtime's state): a marked tree is
// what install/apply created, so removing it is cleanup rather than vandalism.
var stateDirMarkers = []string{
	"veil.env", "state.json", "state.key", "generated", "www", "panel",
	"tls", "certs", "backup.passphrase", "backups", "staging", "autocert",
	// Veil-managed runtime state layouts (caddy StateDirectory, mita).
	".local", ".config", "certificates", "acme",
}

// veilManagedBaseName reports whether a directory name is one Veil provisions
// for itself or its managed runtimes (the packaged defaults plus generated
// unit drop-in dirs such as veil-backup.service.d).
func veilManagedBaseName(base string) bool {
	return base == "veil" || base == "caddy" || base == "mita" ||
		strings.HasPrefix(base, "veil-") || strings.HasPrefix(base, "veil.")
}

// ensureRemovablePath guards operator-supplied directories before they reach
// os.RemoveAll: --etc-dir/--var-dir/--caddy-state-dir/--mita-state-dir land
// here verbatim, and without a guard `veil uninstall --var-dir /` would
// escalate into rm -rf of a system tree (issue #1025). A path is removable
// when it is absent or not a directory (RemoveAll then touches only the node),
// when it carries a Veil-owned marker, when its basename is a Veil-managed
// name, or when it is empty — anything else fails closed for manual review.
func ensureRemovablePath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		// Absent paths are a no-op for RemoveAll; other stat errors surface
		// through the real remover.
		return nil
	}
	if !info.IsDir() {
		// Regular files and symlinks are single-node removals; os.RemoveAll on
		// a symlink deletes the link, never the target.
		return nil
	}
	clean := filepath.Clean(path)
	if clean == filepath.Dir(clean) {
		return fmt.Errorf("refusing to remove filesystem root %s", path)
	}
	if veilManagedBaseName(filepath.Base(clean)) {
		return nil
	}
	for _, marker := range stateDirMarkers {
		if _, err := os.Stat(filepath.Join(clean, marker)); err == nil {
			return nil
		}
	}
	// An empty directory holds nothing to lose — a half-created custom
	// --etc-dir is still removed.
	if entries, err := os.ReadDir(clean); err == nil && len(entries) == 0 {
		return nil
	}
	return fmt.Errorf("refusing to remove %s: directory is not Veil-managed (no marker found); remove it manually or pass --keep-data", path)
}

func (a Actions) ReloadSystemdDaemon() error {
	if err := a.run("daemon-reload", []string{"systemctl", "daemon-reload"}); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	return nil
}

func (a Actions) run(_ string, command []string) error {
	out := a.runner.Run(veilruntime.RuntimeCommandInput{Command: command, Timeout: 30 * time.Second})
	if out.Err != nil {
		return out.Err
	}
	return nil
}
