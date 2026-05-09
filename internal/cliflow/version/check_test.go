package version

import (
	"bytes"
	"strings"
	"testing"
)

func TestCheckReportsNewerRelease(t *testing.T) {
	var out bytes.Buffer
	check := NewCheck("v0.3.16", &out, func() (string, error) { return "v0.3.17", nil })
	if err := check.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "Newer release available: v0.3.16 → v0.3.17") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestCompareVersionsIgnoresVPrefix(t *testing.T) {
	if Compare("v1.2.0", "1.3.0") >= 0 || Compare("v1.4.0", "1.3.0") <= 0 || Compare("v1.3.0", "1.3.0") != 0 {
		t.Fatal("unexpected version comparisons")
	}
}
