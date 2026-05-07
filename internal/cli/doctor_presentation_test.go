package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestDoctorPresentationRendersHumanSummary(t *testing.T) {
	summary := doctorSummary{Version: "test", Runtime: "linux/amd64", Ready: true, Commands: []doctorCommandStatus{{Name: "caddy", Path: "/bin/caddy", Present: true}}}
	var out bytes.Buffer
	if err := NewDoctorPresentation(&out).Render(summary, false); err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{"Veil doctor", "Version: test", "Ready: yes", "- caddy: /bin/caddy"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}
