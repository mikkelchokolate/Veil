package service

import (
	"errors"
	"testing"
	"time"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type fakeLogRunner2 struct {
	out   veilruntime.RuntimeCommandOutput
	input veilruntime.RuntimeCommandInput
}

func (r *fakeLogRunner2) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	r.input = input
	return r.out
}

func TestNewLogReaderDefaultsRunner(t *testing.T) {
	reader := NewLogReader(nil)
	if reader.runner == nil {
		t.Fatal("expected default runner when nil is passed")
	}
}

func TestLogReaderFailureBranches(t *testing.T) {
	tests := []struct {
		name      string
		out       veilruntime.RuntimeCommandOutput
		wantError string
	}{
		{
			name:      "not found without output",
			out:       veilruntime.RuntimeCommandOutput{NotFound: true, Err: errors.New("journalctl not found")},
			wantError: "failed to read logs: journalctl not found",
		},
		{
			name:      "timed out without output",
			out:       veilruntime.RuntimeCommandOutput{TimedOut: true, Err: errors.New("context deadline exceeded")},
			wantError: "failed to read logs: context deadline exceeded",
		},
		{
			name:      "error without output",
			out:       veilruntime.RuntimeCommandOutput{Err: errors.New("exit status 1")},
			wantError: "failed to read logs: exit status 1",
		},
		{
			name:      "failure with output",
			out:       veilruntime.RuntimeCommandOutput{Output: "permission denied", Err: errors.New("exit status 1")},
			wantError: "failed to read logs: permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := NewLogReader(&fakeLogRunner2{out: tt.out})
			_, err := reader.Read("veil-mieru", 25)
			if err == nil || err.Error() != tt.wantError {
				t.Fatalf("err = %v, want %q", err, tt.wantError)
			}
		})
	}
}

func TestLogReaderPassesTimeout(t *testing.T) {
	runner := &fakeLogRunner2{out: veilruntime.RuntimeCommandOutput{Output: "logs"}}
	reader := NewLogReader(runner)
	if _, err := reader.Read("veil-mieru", 10); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if runner.input.Timeout != 10*time.Second {
		t.Fatalf("Timeout = %v, want 10s", runner.input.Timeout)
	}
	want := []string{"journalctl", "-u", "veil-mieru.service", "--no-pager", "-n", "10", "-o", "short-iso"}
	if !sameCommand(runner.input.Command, want) {
		t.Fatalf("command = %v, want %v", runner.input.Command, want)
	}
}

func TestValidLogUnitEdgeCases(t *testing.T) {
	tests := []struct {
		unit string
		want bool
	}{
		{"", false},
		{"veil-mieru", true},
		{"veil_mieru", true},
		{"veil.mieru", true},
		{"veil@mieru", true},
		{"veil/mieru", false},
		{"veil mieru", false},
	}
	for _, tt := range tests {
		t.Run(tt.unit, func(t *testing.T) {
			if got := ValidLogUnit(tt.unit); got != tt.want {
				t.Fatalf("ValidLogUnit(%q) = %v, want %v", tt.unit, got, tt.want)
			}
		})
	}
}
