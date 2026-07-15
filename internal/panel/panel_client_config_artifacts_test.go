package panel

import (
	"strings"
	"testing"
)

func TestPanelClientLinksCardRendersClientConfigArtifactButton(t *testing.T) {
	card := panelClientLinksCardHTML()
	for _, want := range []string{"Download client configs", "download-client-configs"} {
		if !strings.Contains(card, want) {
			t.Fatalf("Client links card missing %q:\n%s", want, card)
		}
	}
}

func TestPanelClientLinksActionsCanDownloadClientConfigArtifacts(t *testing.T) {
	actions := panelClientLinksActionsJS()
	for _, want := range []string{"downloadClientConfigArtifacts", "artifact.kind === 'client_config'", "artifact.content", "veil-client-configs.json"} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Client config artifact download missing %q", want)
		}
	}
}
