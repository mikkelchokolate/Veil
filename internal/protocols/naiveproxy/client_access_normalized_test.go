package naiveproxy

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildLinksUsesNormalizedRuntimeCredentials(t *testing.T) {
	links, err := BuildLinks(model.Settings{Domain: "proxy.example.test"}, model.Inbound{
		Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true,
		NaiveUsername: "legacy", NaivePassword: "legacy-secret",
		RuntimeCredentials: []model.RuntimeCredential{{
			Name: "normalized-client", Username: "normalized_identity", Password: "normalized-secret",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("links=%d, want one normalized link", len(links))
	}
	if !strings.Contains(links[0].URI, "normalized_identity:normalized-secret@") {
		t.Fatalf("normalized runtime credential missing from URI: %q", links[0].URI)
	}
	if strings.Contains(links[0].URI, "legacy") {
		t.Fatalf("inactive legacy credential leaked into URI: %q", links[0].URI)
	}
}
