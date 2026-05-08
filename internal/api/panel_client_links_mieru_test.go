package api

import (
	"strings"
	"testing"
)

func TestPanelClientLinksMentionsMieruClientConfigs(t *testing.T) {
	card := panelClientLinksCardHTML()
	for _, want := range []string{"Mieru", "client config", "download-mieru-configs"} {
		if !strings.Contains(card, want) {
			t.Fatalf("Client links card missing %q:\n%s", want, card)
		}
	}
}

func TestPanelClientLinksActionsCanDownloadMieruConfigs(t *testing.T) {
	actions := panelClientLinksActionsJS()
	for _, want := range []string{"downloadMieruConfigs", "artifact.content", "mieru-client-configs.json"} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Client links actions missing %q", want)
		}
	}
}
