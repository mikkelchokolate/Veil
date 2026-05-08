package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestInstallRejectsMieruStackBecauseProtocolsArePanelInbounds(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install", "--stack", "mieru", "--dry-run"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "Veil install only installs Panel") {
		t.Fatalf("expected Mieru stack rejection, got %v\n%s", err, out.String())
	}
}
