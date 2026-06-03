package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type LogResult struct {
	Unit   string `json:"unit"`
	Output string `json:"output"`
}

type LogReader struct {
	runner RuntimeCommandRunner
}

func NewLogReader(runner RuntimeCommandRunner) LogReader {
	if runner == nil {
		runner = veilruntime.NewRuntimeCommandExecutor()
	}
	return LogReader{runner: runner}
}

func (r LogReader) Read(unit string, lines int) (LogResult, error) {
	if !ValidLogUnit(unit) {
		return LogResult{}, errors.New("invalid unit name")
	}
	if lines < 1 || lines > 500 {
		return LogResult{}, errors.New("lines must be 1-500")
	}
	command := []string{"journalctl", "-u", unit + ".service", "--no-pager", "-n", strconv.Itoa(lines), "-o", "short-iso"}
	out := r.runner.Run(veilruntime.RuntimeCommandInput{Command: command, Timeout: 10 * time.Second})
	if out.Err != nil || out.NotFound || out.TimedOut {
		message := strings.TrimSpace(out.Output)
		if message == "" {
			if out.NotFound {
				message = "journalctl not found"
			} else if out.TimedOut {
				message = "context deadline exceeded"
			} else {
				message = out.Err.Error()
			}
		}
		return LogResult{}, fmt.Errorf("failed to read logs: %s", message)
	}
	return LogResult{Unit: unit, Output: out.Output}, nil
}

func ValidLogUnit(unit string) bool {
	if unit == "" {
		return false
	}
	for _, r := range unit {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '@' || r == '.') {
			return false
		}
	}
	return true
}
