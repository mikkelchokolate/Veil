package api

import (
	"strings"
	"testing"
)

func TestPanelMieruDownloadUsesClientArtifacts(t *testing.T) {
	actions := panelClientLinksActionsJS()
	for _, want := range []string{
		`body.artifacts`,
		`artifact.protocol === 'mieru'`,
		`artifact.content`,
	} {
		if !strings.Contains(actions, want) {
			t.Fatalf("Mieru download should use artifacts missing %q:\n%s", want, actions)
		}
	}
	if strings.Contains(actions, `(body.links || []).filter(link => link.protocol === 'mieru' && link.config)`) {
		t.Fatalf("Mieru download should not filter links.config directly:\n%s", actions)
	}
}
