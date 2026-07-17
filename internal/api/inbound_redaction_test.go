package api

import (
	"testing"

	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

func TestRedactInboundMasksAllCredentialFields(t *testing.T) {
	in := Inbound{
		Name:              "x",
		Password:          "secret-pw",
		NaivePassword:     "secret-naive",
		Hysteria2Password: "secret-hy2",
		ProtocolFields: map[string]any{
			"password":          "secret-pw",
			"naivePassword":     "secret-naive",
			"hysteria2Password": "secret-hy2",
			"port":              443,
		},
	}
	out := redactInbound(in)
	if out.Password != veilsettings.RedactedSecret {
		t.Errorf("Password not redacted: %q", out.Password)
	}
	if out.NaivePassword != veilsettings.RedactedSecret {
		t.Errorf("NaivePassword not redacted: %q", out.NaivePassword)
	}
	if out.Hysteria2Password != veilsettings.RedactedSecret {
		t.Errorf("Hysteria2Password not redacted: %q", out.Hysteria2Password)
	}
	for _, k := range []string{"password", "naivePassword", "hysteria2Password"} {
		if got := out.ProtocolFields[k]; got != veilsettings.RedactedSecret {
			t.Errorf("ProtocolFields[%s] not redacted: %v", k, got)
		}
	}
	if out.ProtocolFields["port"] != 443 {
		t.Errorf("non-secret field changed: %v", out.ProtocolFields["port"])
	}
	// Input must not be mutated.
	if in.Password != "secret-pw" || in.ProtocolFields["password"] != "secret-pw" {
		t.Errorf("redactInbound mutated its input")
	}
}

func TestRedactInboundKeepsEmptySecretsEmpty(t *testing.T) {
	out := redactInbound(Inbound{Name: "x"})
	if out.Password != "" || out.NaivePassword != "" || out.Hysteria2Password != "" {
		t.Errorf("empty secrets should stay empty, got %+v", out)
	}
}

func TestRedactInboundList(t *testing.T) {
	if redactInboundList(nil) != nil {
		t.Fatal("nil list should stay nil")
	}
	out := redactInboundList([]Inbound{{Name: "a", Password: "p"}, {Name: "b"}})
	if len(out) != 2 || out[0].Password != veilsettings.RedactedSecret || out[1].Password != "" {
		t.Errorf("unexpected list redaction: %+v", out)
	}
}
