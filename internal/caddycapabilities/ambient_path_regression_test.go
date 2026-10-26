package caddycapabilities

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/syspath"
)

// TestProbeIgnoresAmbientPATH locks #1458: the probe is reached from root
// `veil install` and `veil repair` with an empty binary path, and it used to
// run the bare name through the caller's PATH. A planted `caddy` shim earlier
// in PATH must never execute.
func TestProbeIgnoresAmbientPATH(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "shim-ran")
	shim := filepath.Join(dir, "caddy")
	body := "#!/bin/sh\nprintf 'ran' > " + marker + "\nprintf '%s' '[{\"module_name\":\"http.handlers.forward_proxy\"}]'\n"
	if err := os.WriteFile(shim, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+syspath.TrustedPATH())

	_, err := Probe("")
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("the planted caddy shim ran as root; probe error = %v", err)
	}
	// Without a caddy in the trusted directories the probe must report a
	// missing binary, which panel-only installs tolerate.
	if err == nil {
		t.Log("a real caddy exists in the trusted directories; the shim still did not run")
		return
	}
	if !IsMissingBinary(err) {
		t.Fatalf("probe error %v is not reported as a missing binary", err)
	}
}

// TestProbeStillRunsAnExplicitBinary keeps the explicit-path callers working
// after the syspath move (a caller that resolved caddy itself passes an
// absolute path).
func TestProbeStillRunsAnExplicitBinary(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "caddy")
	body := "#!/bin/sh\nprintf '%s' '[{\"module_name\":\"http.handlers.forward_proxy\"},{\"module_name\":\"http\"}]'\n"
	if err := os.WriteFile(binary, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	capabilities, err := Probe(binary)
	if err != nil {
		t.Fatalf("Probe(%q): %v", binary, err)
	}
	if !capabilities.ForwardProxy || !capabilities.HTTP3 {
		t.Fatalf("capabilities = %+v", capabilities)
	}
}
