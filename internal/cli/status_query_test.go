package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestStatusQueryRendersHumanStatus(t *testing.T) {
	status := &statusResponse{Version: "test", Mode: "server", Services: []serviceStatus{{Name: "veil", ActiveState: "active"}}}
	var out bytes.Buffer
	if err := NewStatusQuery(statusQueryOptions{JSON: false}, &out).Render(status); err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Veil test", "Mode: server", "● veil: active"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}
