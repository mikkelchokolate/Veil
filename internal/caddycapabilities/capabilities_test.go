package caddycapabilities

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestIsMissingBinary(t *testing.T) {
	if IsMissingBinary(nil) {
		t.Fatal("nil is not a missing binary")
	}
	if !IsMissingBinary(exec.ErrNotFound) {
		t.Fatal("exec.ErrNotFound should be missing binary")
	}
	if !IsMissingBinary(fs.ErrNotExist) {
		t.Fatal("fs.ErrNotExist should be missing binary")
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
	_, err := Probe("veil-test-missing-caddy-not-on-path")
	if err == nil {
		t.Fatal("expected probe error for missing binary")
	}
	if !IsMissingBinary(err) {
		t.Fatalf("IsMissingBinary(%v) = false", err)
	}
	_, pathErr := Probe("/veil-test-missing-caddy")
	if pathErr == nil {
		t.Fatal("expected probe error for missing path")
	}
	if !IsMissingBinary(pathErr) {
		t.Fatalf("IsMissingBinary(%v) = false", pathErr)
	}
}

// TestProbeParsesModuleList exercises the same parseModuleList path that
// Probe calls, so the locked behavior is the production path, not a dead
// copy (#882). HTTP3 must be derived from the base "http" module — the old
// parseModuleList never set it, silently reporting HTTP3=false.
func TestProbeParsesModuleList(t *testing.T) {
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
	if !caps.HTTP3 {
		t.Error("expected HTTP3=true when the base http module is listed")
	}
}

func TestParseModuleListWithoutHTTPModule(t *testing.T) {
	caps, err := parseModuleList([]byte(`[{"module_name":"http.handlers.forward_proxy"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if !caps.ForwardProxy {
		t.Error("expected ForwardProxy=true")
	}
	if caps.HTTP3 {
		t.Error("expected HTTP3=false when the base http module is absent")
	}
}

// TestProbeRunsLiveBinary locks the end-to-end contract: Probe must execute
// the binary and parse its real list-modules output, not a canned code path.
func TestProbeRunsLiveBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake caddy is a POSIX shell script")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "caddy")
	script := "#!/bin/sh\nprintf '%s' '[{\"module_name\":\"http.handlers.forward_proxy\"},{\"module_name\":\"http\"}]'\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	caps, err := Probe(binary)
	if err != nil {
		t.Fatalf("Probe(fake caddy): %v", err)
	}
	if !caps.ForwardProxy || !caps.HTTP3 {
		t.Fatalf("Probe capabilities = %+v, want ForwardProxy+HTTP3", caps)
	}
}

// A wedged `caddy list-modules` must not deadlock the apply path: the probe
// honors the caller's deadline (and its own internal timeout) instead of
// blocking forever (#1141).
func TestProbeReturnsWhenBinaryHangs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake caddy is a POSIX shell script")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "caddy")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexec sleep 120\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := ProbeContext(ctx, binary)
	if err == nil {
		t.Fatal("hanging caddy must report a probe error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("ProbeContext blocked %s; the caller deadline was ignored", elapsed)
	}
}

// A binary that streams unbounded output must not exhaust panel memory: the
// probe caps stdout and fails instead of buffering forever (#1141).
func TestProbeCapsModuleListOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake caddy is a POSIX shell script")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "caddy")
	script := "#!/bin/sh\nhead -c 6000000 /dev/zero | tr '\\000' 'x'\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := Probe(binary)
	if err == nil {
		t.Fatal("oversized list-modules output must be rejected")
	}
	if IsMissingBinary(err) {
		t.Fatalf("oversized output must not masquerade as a missing binary: %v", err)
	}
}
