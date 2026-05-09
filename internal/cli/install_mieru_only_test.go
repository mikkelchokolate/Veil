package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstallRejectsRemovedStackFlag(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--stack", "mieru", "--dry-run"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --stack") {
		t.Fatalf("expected --stack to be removed, got %v\n%s", err, out.String())
	}
}
