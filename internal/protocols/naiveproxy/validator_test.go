package naiveproxy

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// issueKeys flattens issues to "field|code" for exact multiset assertions.
func issueKeys(issues []model.ValidationIssue) []string {
	keys := make([]string, 0, len(issues))
	for _, i := range issues {
		keys = append(keys, i.Field+"|"+i.Code)
	}
	return keys
}

func TestValidateInboundMissingDomain(t *testing.T) {
	v := Validator{}
	issues := v.ValidateInbound(model.Settings{}, model.Inbound{Protocol: "naiveproxy", ProtocolFields: map[string]any{}})
	// An empty inbound must report exactly the three absent requirements —
	// domain, ACME email, credential — each with its dedicated code (#821).
	want := []string{
		model.InboundDomainField + "|naive_domain_required",
		model.InboundEmailField + "|naive_email_required",
		"profiles|naive_credential_required",
	}
	got := issueKeys(issues)
	if len(got) != len(want) {
		t.Fatalf("issues = %+v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("issues[%d] = %q, want %q (all: %+v)", i, got[i], want[i], issues)
		}
	}
	for _, i := range issues {
		if i.Severity != "error" || i.Source != "naiveproxy" || i.Message == "" {
			t.Fatalf("issue fields incomplete: %+v", i)
		}
	}
}

func TestValidateInboundInvalidTransport(t *testing.T) {
	v := Validator{}
	inbound := model.Inbound{
		Protocol: "naiveproxy",
		ProtocolFields: map[string]any{
			"domain":    "x.com",
			"transport": "udp",
		},
	}
	issues := v.ValidateInbound(model.Settings{}, inbound)
	// udp is the only invalid input: the transport issue must fire with its
	// exact code; email+credential also fire (independent of transport), the
	// valid domain must not produce a domain issue (#821).
	want := []string{
		model.InboundEmailField + "|naive_email_required",
		"transport|naive_transport_invalid",
		"profiles|naive_credential_required",
	}
	got := issueKeys(issues)
	if len(got) != len(want) {
		t.Fatalf("issues = %+v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("issues[%d] = %q, want %q (all: %+v)", i, got[i], want[i], issues)
		}
	}
	for _, i := range issues {
		if i.Field == "transport" && i.Code != "naive_transport_invalid" {
			t.Fatalf("transport issue has wrong code: %+v", i)
		}
	}
}

func TestValidateInboundCleanInboundHasNoIssues(t *testing.T) {
	v := Validator{}
	inbound := model.Inbound{
		Protocol: "naiveproxy",
		ProtocolFields: map[string]any{
			"domain":    "x.com",
			"email":     "admin@x.com",
			"transport": "tcp",
		},
		Profiles: []model.ClientProfile{{Name: "u", Enabled: true, Username: "u", Password: "p"}},
	}
	if issues := v.ValidateInbound(model.Settings{}, inbound); len(issues) != 0 {
		t.Fatalf("clean inbound must produce no issues, got %+v", issues)
	}
}
