package caddycapabilities

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
)

func TestIsMissingBinary(t *testing.T) {
	if IsMissingBinary(nil) {
		t.Fatal("nil is not a missing binary")
	}
	if !IsMissingBinary(exec.ErrNotFound) {
		t.Fatal("exec.ErrNotFound should be missing binary")
	}
	wrapped := fmt.Errorf("caddy list-modules failed: %w", exec.ErrNotFound)
	if !IsMissingBinary(wrapped) {
		t.Fatal("wrapped ErrNotFound should be missing binary")
	}
	if IsMissingBinary(errors.New("caddy crashed")) {
		t.Fatal("unrelated error should not be missing binary")
	}
}

func TestProbeMissingBinary(t *testing.T) {
	_, err := Probe("/veil-test-missing-caddy")
	if err == nil {
		t.Fatal("expected probe error for missing binary")
	}
	if !IsMissingBinary(err) {
		t.Fatalf("IsMissingBinary(%v) = false", err)
	}
}

func TestProbeParsesModuleList(t *testing.T) {
	// A mock binary that prints a module list matching Caddy's `caddy list-modules --json` shape.
	// For the plan we test parsing of a known JSON fragment.
	input := `[
	  {"module_name":"http.handlers.forward_proxy"},
	  {"module_name":"http"}
	]`
	caps, err := parseModuleList([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if !caps.ForwardProxy {
		t.Error("expected ForwardProxy=true")
	}
}
