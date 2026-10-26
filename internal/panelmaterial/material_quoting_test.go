package panelmaterial

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/veilenv"
)

// TestEnvContentQuotesLikeTheWriter locks the round-trip with the parser: a
// state key or panel value holding a space, '#' or a quote must be written the
// way internal/veilenv writes it, so reading the file back with veilenv.Value
// returns the original value instead of one with the quotes trimmed off
// (#1485 review).
func TestEnvContentQuotesLikeTheWriter(t *testing.T) {
	input := Input{
		Paths:          Paths{EtcDir: "/etc/veil dir", VarDir: "/var/lib/veil"},
		PanelAuthToken: "token",
		KeyPath:        "/etc/veil dir/secrets dir/rotated state.key",
		StatePath:      "/var/lib/veil dir/state.json",
	}
	env, err := NewManagedMaterial(input).EnvContent()
	if err != nil {
		t.Fatalf("EnvContent: %v", err)
	}
	body := []byte(env)
	if got := veilenv.Value(body, veilenv.KeyPathKey); got != input.KeyPath {
		t.Fatalf("round-trip VEIL_KEY_PATH = %q, want %q\n%s", got, input.KeyPath, env)
	}
	if got := veilenv.Value(body, "VEIL_STATE_PATH"); got != input.StatePath {
		t.Fatalf("round-trip VEIL_STATE_PATH = %q, want %q\n%s", got, input.StatePath, env)
	}
}
