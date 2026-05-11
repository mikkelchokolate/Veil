package install

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmPlanAcceptsInteractiveYes(t *testing.T) {
	var out bytes.Buffer
	if err := ConfirmPlan(strings.NewReader("y\n"), &out, true); err != nil {
		t.Fatalf("ConfirmPlan: %v", err)
	}
	if !strings.Contains(out.String(), "Apply install plan? [y/N]:") {
		t.Fatalf("confirmation prompt missing:\n%s", out.String())
	}
}

func TestConfirmPlanRejectsNonInteractiveWithoutYes(t *testing.T) {
	if err := ConfirmPlan(strings.NewReader(""), &bytes.Buffer{}, false); err == nil || !strings.Contains(err.Error(), "apply mode requires --yes") {
		t.Fatalf("expected --yes error, got %v", err)
	}
}
