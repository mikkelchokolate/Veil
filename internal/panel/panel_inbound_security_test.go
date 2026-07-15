package panel

import (
	"strings"
	"testing"
)

func TestInboundTableUsesSafeDOMAndDelegatedActions(t *testing.T) {
	js := panelInboundActionsJS()
	for _, want := range []string{
		`function createInboundRow(inbound)`,
		`name.textContent = String(inbound.name || '')`,
		`button.dataset.inboundAction = action`,
		`event.target.closest('[data-inbound-action]')`,
		`status.addEventListener('change'`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("safe inbound renderer missing %q", want)
		}
	}
	for _, unsafe := range []string{
		`row.innerHTML`,
		`onclick="openEditInboundModal`,
		`onclick="directDeleteInbound`,
		`onchange="toggleInboundActive`,
	} {
		if strings.Contains(js, unsafe) {
			t.Fatalf("inbound table still contains unsafe markup %q", unsafe)
		}
	}
}

func TestInboundEditorKeepsAddAndEditModesDistinct(t *testing.T) {
	js := panelInboundActionsJS()
	for _, want := range []string{
		`window.inboundEditorMode = 'add';`,
		`window.inboundEditorMode = 'edit';`,
		`window.inboundEditorOriginalName = inbound.name;`,
		`const isEdit = window.inboundEditorMode === 'edit';`,
		`method: isEdit ? 'PUT' : 'POST'`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("inbound editor mode contract missing %q", want)
		}
	}
	if strings.Contains(js, `some((inbound) => inbound.name === name)`) {
		t.Fatal("Add mode must not infer PUT from a colliding inbound name")
	}
}

func TestInboundMutationsPreserveUIStateOnFailure(t *testing.T) {
	js := panelInboundActionsJS()
	for _, want := range []string{
		`if (updated === null)`,
		`control.checked = !checked;`,
		`if (deleted !== null)`,
		`if (!deleted) return;`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("inbound failure handling missing %q", want)
		}
	}
}
