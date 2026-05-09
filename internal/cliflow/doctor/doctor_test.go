package doctor

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestReadinessTreatsProtocolRuntimesAsOptional(t *testing.T) {
	readiness := NewReadiness("test", func(name string) (string, error) {
		if name == "systemctl" {
			return "/bin/systemctl", nil
		}
		return "", errors.New("missing")
	})
	summary := readiness.Summary()
	if !summary.Ready {
		t.Fatalf("summary should be ready when only optional commands are missing: %+v", summary)
	}
}

func TestPresentationRendersHumanSummary(t *testing.T) {
	summary := Summary{Version: "test", Runtime: "linux/amd64", Ready: true, Commands: []CommandStatus{{Name: "systemctl", Path: "/bin/systemctl", Present: true}}}
	var out bytes.Buffer
	if err := NewPresentation(&out).Render(summary, false); err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"Veil doctor", "Version: test", "Ready: yes", "- systemctl: /bin/systemctl"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}
