package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmInstallPlanAcceptsInteractiveYes(t *testing.T) {
	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("y\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := confirmInstallPlan(cmd, true); err != nil {
		t.Fatalf("confirmInstallPlan: %v", err)
	}
	if !strings.Contains(out.String(), "Apply install plan? [y/N]:") {
		t.Fatalf("confirmation prompt missing:\n%s", out.String())
	}
}

func TestConfirmInstallPlanRejectsNonInteractiveWithoutYes(t *testing.T) {
	cmd := NewRootCommand("test")
	if err := confirmInstallPlan(cmd, false); err == nil || !strings.Contains(err.Error(), "apply mode requires --yes") {
		t.Fatalf("expected --yes error, got %v", err)
	}
}
