package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCheckReportsNewerRelease(t *testing.T) {
	var out bytes.Buffer
	check := NewVersionCheck("v0.3.16", &out)
	check.latest = func() (string, error) { return "v0.3.17", nil }
	if err := check.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "Newer release available: v0.3.16 → v0.3.17") {
		t.Fatalf("output = %s", out.String())
	}
}
