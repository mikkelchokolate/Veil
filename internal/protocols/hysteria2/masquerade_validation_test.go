package hysteria2

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestValidateInboundRejectsInvalidMasqueradeURL locks in audit #64: the
// masquerade URL is rendered verbatim and upstream refuses to start on a
// non-http(s) or host-less URL, so live validation must reject it before
// apply instead of crash-looping the service.
func TestValidateInboundRejectsInvalidMasqueradeURL(t *testing.T) {
	p := Plugin{}
	for _, bad := range []string{"not a url", "/relative/path", "ftp://x.example", "https://", ""} {
		issues := p.ValidateInbound(model.Settings{Domain: "hy.example.com"}, model.Inbound{
			Name:              "hy2",
			Protocol:          "hysteria2",
			Transport:         "udp",
			Port:              443,
			Enabled:           true,
			MasqueradeURL:     bad,
			Hysteria2Password: "pw",
		})
		got := issueByCode(issues, "hysteria2_masquerade_invalid")
		if bad != "" && got == nil {
			t.Errorf("masqueradeURL %q: expected hysteria2_masquerade_invalid, got %+v", bad, issues)
		}
		if bad == "" && got != nil {
			t.Errorf("empty masqueradeURL must be allowed (defaults apply): %+v", issues)
		}
	}
}

// TestValidateInboundAcceptsValidMasqueradeURL ensures http(s) URLs with a
// host pass and do not add the issue.
func TestValidateInboundAcceptsValidMasqueradeURL(t *testing.T) {
	p := Plugin{}
	for _, good := range []string{"https://example.com", "http://bing.com/", "https://example.com/path?q=1"} {
		issues := p.ValidateInbound(model.Settings{Domain: "hy.example.com"}, model.Inbound{
			Name:              "hy2",
			Protocol:          "hysteria2",
			Transport:         "udp",
			Port:              443,
			Enabled:           true,
			MasqueradeURL:     good,
			Hysteria2Password: "pw",
		})
		if got := issueByCode(issues, "hysteria2_masquerade_invalid"); got != nil {
			t.Errorf("masqueradeURL %q rejected: %+v", good, issues)
		}
	}
}

// TestValidateInboundMasqueradeFromProtocolFields verifies the validator
// resolves the masquerade URL through protocolFields like the renderer.
func TestValidateInboundMasqueradeFromProtocolFields(t *testing.T) {
	p := Plugin{}
	issues := p.ValidateInbound(model.Settings{Domain: "hy.example.com"}, model.Inbound{
		Name:              "hy2",
		Protocol:          "hysteria2",
		Transport:         "udp",
		Port:              443,
		Enabled:           true,
		Hysteria2Password: "pw",
		ProtocolFields:    map[string]any{"masqueradeURL": "ftp://bad.example"},
	})
	if got := issueByCode(issues, "hysteria2_masquerade_invalid"); got == nil {
		t.Fatalf("expected masquerade invalid from protocolFields, got %+v", issues)
	}
}

func issueByCode(issues []model.ValidationIssue, code string) *model.ValidationIssue {
	for i := range issues {
		if issues[i].Code == code {
			return &issues[i]
		}
	}
	return nil
}
