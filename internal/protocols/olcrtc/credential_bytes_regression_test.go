package olcrtc

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestOlcrtcKeyPreservesDynamicKeyBytes is the #344 regression: the server
// resolver trimmed the winning key while client export carried the raw
// protocolFields value byte-for-byte. Both sides must agree on the identical
// bytes so the URI's embedded key authenticates against the rendered server
// config.
func TestOlcrtcKeyPreservesDynamicKeyBytes(t *testing.T) {
	inbound := model.Inbound{ProtocolFields: map[string]any{"password": " padded-key "}}
	if got := olcrtcKey(inbound); got != " padded-key " {
		t.Fatalf("olcrtcKey = %q, want the stored dynamic bytes %q", got, " padded-key ")
	}
}

// TestOlcrtcKeyPreservesFlatKeyBytes pins the flat-field branch of the same
// contract: the stored flat key must reach the server config and the client
// URI unchanged.
func TestOlcrtcKeyPreservesFlatKeyBytes(t *testing.T) {
	inbound := model.Inbound{Password: " flat-key "}
	if got := olcrtcKey(inbound); got != " flat-key " {
		t.Fatalf("olcrtcKey = %q, want the stored flat bytes %q", got, " flat-key ")
	}
}

// TestOlcrtcKeyBlankDynamicFallsToFlat keeps the #122 contract: an empty or
// whitespace-only dynamic value counts as unset and must never hide a valid
// stored flat key.
func TestOlcrtcKeyBlankDynamicFallsToFlat(t *testing.T) {
	inbound := model.Inbound{Password: "flat-key", ProtocolFields: map[string]any{"password": "   "}}
	if got := olcrtcKey(inbound); got != "flat-key" {
		t.Fatalf("olcrtcKey = %q, want %q", got, "flat-key")
	}
}
