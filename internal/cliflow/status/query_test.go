package status

import (
	"bytes"
	"strings"
	"testing"
)

func TestQueryRendersHumanStatus(t *testing.T) {
	response := &Response{Version: "test", Mode: "server", Services: []ServiceStatus{{Name: "veil", ActiveState: "active"}}}
	var out bytes.Buffer
	if err := NewQuery(Options{JSON: false}, &out, nil).Render(response); err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Veil test", "Mode: server", "● veil: active"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestCandidateAddrsPreferGeneratedPanelTLSBeforeHTTP(t *testing.T) {
	if got := CandidateAddrs("127.0.0.1:2096"); len(got) != 2 || got[0] != "https://127.0.0.1:2096" || got[1] != "http://127.0.0.1:2096" {
		t.Fatalf("CandidateAddrs = %+v", got)
	}
	if got := CandidateAddrs("http://example.com"); len(got) != 1 || got[0] != "http://example.com" {
		t.Fatalf("CandidateAddrs with scheme = %+v", got)
	}
}
