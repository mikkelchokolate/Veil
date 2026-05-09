package service

import (
	"errors"
	"testing"

	veilruntime "github.com/veil-panel/veil/internal/runtime"
)

type fakeLogRunner struct {
	input veilruntime.RuntimeCommandInput
	out   veilruntime.RuntimeCommandOutput
}

func (r *fakeLogRunner) Run(input veilruntime.RuntimeCommandInput) veilruntime.RuntimeCommandOutput {
	r.input = input
	return r.out
}

func TestLogReaderBuildsSafeJournalctlCommand(t *testing.T) {
	runner := &fakeLogRunner{out: veilruntime.RuntimeCommandOutput{Output: "logs"}}
	result, err := NewLogReader(runner).Read("veil-mieru", 25)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	want := []string{"journalctl", "-u", "veil-mieru.service", "--no-pager", "-n", "25", "-o", "short-iso"}
	if !sameCommand(runner.input.Command, want) {
		t.Fatalf("command = %+v, want %+v", runner.input.Command, want)
	}
	if result.Unit != "veil-mieru" || result.Output != "logs" {
		t.Fatalf("result = %+v", result)
	}
}

func TestLogReaderRejectsInvalidUnitAndLines(t *testing.T) {
	reader := NewLogReader(&fakeLogRunner{})
	if _, err := reader.Read("bad/unit", 50); err == nil || err.Error() != "invalid unit name" {
		t.Fatalf("unit err = %v", err)
	}
	if _, err := reader.Read("veil", 501); err == nil || err.Error() != "lines must be 1-500" {
		t.Fatalf("lines err = %v", err)
	}
}

func TestLogReaderReturnsCommandFailureWithOutput(t *testing.T) {
	reader := NewLogReader(&fakeLogRunner{out: veilruntime.RuntimeCommandOutput{Output: "denied", Err: errors.New("exit status 1")}})
	_, err := reader.Read("veil", 50)
	if err == nil || err.Error() != "failed to read logs: denied" {
		t.Fatalf("err = %v", err)
	}
}

func sameCommand(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
