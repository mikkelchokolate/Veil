package panel

import (
	"strings"
	"testing"
)

func TestPanelRoleVisibilityCompatibilityHookDoesNotWrapGuard(t *testing.T) {
	if got := panelRoleVisibilityJS(); got != "" {
		t.Fatalf("compatibility role visibility hook must be empty, got %q", got)
	}
}

func TestPanelClientProfileActionsDoNotMountDuplicateRoleGuard(t *testing.T) {
	actions := panelClientProfileActionsJS()
	if strings.Contains(actions, `const veilBaseApplyViewerRoleGuard`) {
		t.Fatal("client profile actions must not mount a second role visibility wrapper")
	}
	if strings.Contains(actions, `applyViewerRoleGuard = function()`) {
		t.Fatal("client profile actions must not redefine the shared role guard")
	}
}
