package panel

import (
	"strings"
	"testing"
)

func TestPanelClientConfigArtifactDownloadUsesArtifacts(t *testing.T) {
	actions := panelClientLinksActionsJS()
	for _, want := range []string{
		`body.artifacts`,
		`artifact.kind === 'client_config'`,
		`artifact.content`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Client config artifact download should use artifacts; missing %q:\n%s", want, actions)
		}
	}
	if strings.Contains(actions, `(body.links || []).filter(link => link.protocol === 'mieru' && link.config)`) {
		t.Fatalf("Client config artifact download should not filter links.config directly by protocol:\n%s", actions)
	}
}
