package installer

import (
	"strings"
	"testing"
)

func TestCaddyPanelBuildHintDescribesRuntimePrerequisite(t *testing.T) {
	hint := CaddyPanelBuildHint("/usr/sbin/caddy")
	if hint.BinaryPath != "/usr/sbin/caddy" {
		t.Fatalf("unexpected binary path: %+v", hint)
	}
	if len(hint.Commands) != 1 || !strings.Contains(hint.Commands[0], "requires standard Caddy at /usr/sbin/caddy") {
		t.Fatalf("Panel Caddy hint should describe prerequisite, not an installer side effect: %+v", hint.Commands)
	}
}
