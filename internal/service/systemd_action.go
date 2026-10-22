package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mikkelchokolate/Veil/internal/syspath"
)

type SystemdActionModule struct {
	units []string
}

type CommandRunnerFunc func(command string, args ...string) error

func (f CommandRunnerFunc) Run(command string, args ...string) error { return f(command, args...) }

func NewSystemdActionModule(units []string) SystemdActionModule {
	return SystemdActionModule{units: units}
}

func (m SystemdActionModule) Plan() []SystemdAction {
	clean := make([]string, 0, len(m.units))
	for _, unit := range m.units {
		if unit != "" {
			clean = append(clean, unit)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	actions := []SystemdAction{{Command: "systemctl", Args: []string{"daemon-reload"}}}
	for _, unit := range clean {
		actions = append(actions, SystemdAction{Command: "systemctl", Args: []string{"enable", unit}})
	}
	// Queue dependent units before waiting, so their restart jobs can merge
	// instead of stopping/starting the panel between separate commands.
	actions = append(actions, SystemdAction{Command: "systemctl", Args: append([]string{"restart"}, clean...)})
	return actions
}

func (SystemdActionModule) Run(runner CommandRunner, actions []SystemdAction) error {
	for _, action := range actions {
		if err := runner.Run(action.Command, action.Args...); err != nil {
			return fmt.Errorf("%s %v: %w", action.Command, action.Args, err)
		}
	}
	return nil
}

type SystemdExecRunner struct{}

func (SystemdExecRunner) Run(command string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), systemdCommandTimeout(command, args...))
	defer cancel()
	cmd := syspath.CommandContext(ctx, command, args...)
	output := &tailBuffer{limit: systemdOutputLimit}
	cmd.Stdout = output
	cmd.Stderr = output
	// Output now flows through pipes, and Wait reads them to EOF. A descendant
	// that inherits them (an agent systemctl spawns) must not hold Run past
	// its timeout, so stop reading shortly after the process exits.
	cmd.WaitDelay = systemdWaitDelay
	if err := cmd.Run(); err != nil {
		// A batched `systemctl restart a b c` reports only "exit status 1";
		// systemctl's own message names the unit that failed, so keep its tail.
		if detail := output.String(); detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}

// systemdWaitDelay bounds how long Run waits for output pipes held open by
// orphaned descendants after systemctl itself has exited or been killed.
const systemdWaitDelay = 5 * time.Second

// systemdOutputLimit bounds how much systemctl output is kept for an error.
const systemdOutputLimit = 2048

// tailBuffer keeps the last limit bytes written to it, where systemctl and
// journal hints put the failing unit.
type tailBuffer struct {
	mu        sync.Mutex
	limit     int
	buf       []byte
	truncated bool
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if over := len(b.buf) - b.limit; over > 0 {
		b.buf = append(b.buf[:0], b.buf[over:]...)
		b.truncated = true
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Collapse lines into one and drop invalid UTF-8 bytes anywhere, including
	// a rune split by the head cut.
	text := strings.Join(strings.Fields(strings.ToValidUTF8(string(b.buf), "")), " ")
	if text == "" {
		return ""
	}
	if b.truncated {
		return "..." + text
	}
	return text
}

func systemdCommandTimeout(command string, args ...string) time.Duration {
	timeout := 10 * time.Second
	if command == "systemctl" && len(args) > 0 {
		switch args[0] {
		case "daemon-reload", "enable", "disable":
			timeout = 10 * time.Second
		case "restart":
			// Preserve the old per-unit budget when restart jobs are queued
			// together. Flags such as --no-block are not units. A flag value
			// passed as a separate arg (--job-mode replace) would count as a
			// unit; that only over-budgets, and Plan never passes one.
			units := 0
			for _, arg := range args[1:] {
				if !strings.HasPrefix(arg, "-") {
					units++
				}
			}
			timeout = 30 * time.Second * time.Duration(max(1, units))
		}
	}
	return timeout
}
