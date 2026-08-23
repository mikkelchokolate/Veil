package uninstall

import (
	"fmt"
	"os"
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
	return a.fileRemover(path)
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
