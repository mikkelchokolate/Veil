package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

type RuntimeCommandInput struct {
	Command []string
	Timeout time.Duration
	Env     []string
}

type RuntimeCommandOutput struct {
	Command  []string
	Output   string
	Err      error
	Empty    bool
	NotFound bool
	TimedOut bool
}

func mergeCommandEnv(base, overrides []string) []string {
	overridden := make(map[string]struct{}, len(overrides))
	for _, item := range overrides {
		key, _, _ := strings.Cut(item, "=")
		overridden[key] = struct{}{}
	}
	merged := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if _, replaced := overridden[key]; !replaced {
			merged = append(merged, item)
		}
	}
	return append(merged, overrides...)
}

type RuntimeCommandExecutor struct{}

func NewRuntimeCommandExecutor() RuntimeCommandExecutor { return RuntimeCommandExecutor{} }

func (RuntimeCommandExecutor) Run(input RuntimeCommandInput) RuntimeCommandOutput {
	command := append([]string(nil), input.Command...)
	result := RuntimeCommandOutput{Command: command}
	if len(command) == 0 {
		result.Empty = true
		result.Err = errors.New("command is empty")
		return result
	}
	binary, err := exec.LookPath(command[0])
	if err != nil {
		result.NotFound = true
		result.Err = errors.New(command[0] + " not found")
		return result
	}
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, command[1:]...)
	if len(input.Env) > 0 {
		cmd.Env = mergeCommandEnv(os.Environ(), input.Env)
	}
	out, err := cmd.CombinedOutput()
	result.Output = strings.TrimSpace(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.Err = ctx.Err()
		return result
	}
	result.Err = err
	return result
}
