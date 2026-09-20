package hysteria2

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestHysteria2PasswordPreservesInboundPasswordBytes is the #331 regression:
// the server-side resolver used to TrimSpace(inbound.Password) while client
// export embedded the raw stored value, so an explicitly entered password
// with surrounding whitespace rendered a different credential than the URI
// advertised. The winning value must be preserved byte-for-byte.
func TestHysteria2PasswordPreservesInboundPasswordBytes(t *testing.T) {
	inbound := model.Inbound{Password: " padded-pass "}
	if got := hysteria2Password(model.Settings{}, inbound); got != " padded-pass " {
		t.Fatalf("hysteria2Password = %q, want the stored bytes %q", got, " padded-pass ")
	}
}

// TestHysteria2PasswordWhitespaceOnlyFallsThrough keeps the empty-check
// contract: a whitespace-only inbound password counts as unset and must not
// shadow the dynamic or settings-level credential.
func TestHysteria2PasswordWhitespaceOnlyFallsThrough(t *testing.T) {
	inbound := model.Inbound{Password: "  ", ProtocolFields: map[string]any{"hysteria2Password": "dynamic-secret"}}
	if got := hysteria2Password(model.Settings{}, inbound); got != "dynamic-secret" {
		t.Fatalf("hysteria2Password = %q, want %q", got, "dynamic-secret")
	}
}
