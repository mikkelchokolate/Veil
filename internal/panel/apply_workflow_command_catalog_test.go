package panel

import "testing"

func TestApplyWorkflowCommandCatalogNamesPanelApplyStages(t *testing.T) {
	commands := NewApplyWorkflowCommandCatalog().Commands()
	want := map[string]ApplyRequest{
		"stage":                 {Confirm: true},
		"promote-live":          {Confirm: true, ApplyLive: true},
		"promote-live-services": {Confirm: true, ApplyLive: true, ApplyServices: true},
	}
	seen := map[string]ApplyRequest{}
	for _, command := range commands {
		seen[command.Name] = command.Request
	}
	for name, request := range want {
		if seen[name] != request {
			t.Fatalf("command %q = %+v, want %+v; all=%+v", name, seen[name], request, commands)
		}
	}
}

func TestApplyWorkflowCommandCatalogRendersPanelActions(t *testing.T) {
	js := NewApplyWorkflowCommandCatalog().PanelActionsJS()
	for _, want := range []string{"build-apply-plan", "apply-staged-files", "apply-live-configs", "reload-services", "runApplyWorkflowCommand"} {
		if !containsStringText(js, want) {
			t.Fatalf("Panel apply command JS missing %q:\n%s", want, js)
		}
	}
}

func containsStringText(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
