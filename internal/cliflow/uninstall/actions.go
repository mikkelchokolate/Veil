package uninstall

import (
	"fmt"
	"os"
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

func DefaultDependencies() Dependencies {
	actions := NewActions(nil, nil)
	return Dependencies{ServiceStopper: actions.StopAndDisableService, FileRemover: actions.RemovePath, SystemdReloader: actions.ReloadSystemdDaemon}
}

func StopAndDisableService(service string) error {
	return NewActions(nil, nil).StopAndDisableService(service)
}

func RemovePath(path string) error {
	return NewActions(nil, nil).RemovePath(path)
}

func ReloadSystemdDaemon() error {
	return NewActions(nil, nil).ReloadSystemdDaemon()
}

func (a Actions) StopAndDisableService(service string) error {
	if err := a.run("stop", []string{"systemctl", "stop", service}); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	if err := a.run("disable", []string{"systemctl", "disable", service}); err != nil {
		return fmt.Errorf("disable: %w", err)
	}
	return nil
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
