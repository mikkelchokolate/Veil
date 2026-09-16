package generatedconfig

import "testing"

// TestMieruFallbackPasswordPreservedByteForByte covers audit #311: a manually
// entered fallback password with surrounding whitespace is persisted
// unchanged, so the server must render the same bytes the client export
// carries — no trimming on only one side.
func TestMieruFallbackPasswordPreservedByteForByte(t *testing.T) {
	config, ok, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{{
		Name: "m", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true,
		Password: " Fixture-Password-123 ",
	}})
	if err != nil || !ok {
		t.Fatalf("Build: ok=%v err=%v", ok, err)
	}
	if len(config.Users) != 1 || config.Users[0].Password != " Fixture-Password-123 " {
		t.Fatalf("server users = %+v, want byte-for-byte fallback password", config.Users)
	}
}

// TestMieruFallbackPasswordDynamicWinsByteForByte ensures the dynamic
// protocolFields password wins over the flat field without trimming, matching
// the exported client credential bytes (audit #311).
func TestMieruFallbackPasswordDynamicWinsByteForByte(t *testing.T) {
	config, ok, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{{
		Name: "m", Protocol: "mieru", Transport: "udp", Port: 443, Enabled: true,
		Password:       "flat",
		ProtocolFields: map[string]any{"password": " dyn "},
	}})
	if err != nil || !ok {
		t.Fatalf("Build: ok=%v err=%v", ok, err)
	}
	if len(config.Users) != 1 || config.Users[0].Password != " dyn " {
		t.Fatalf("server users = %+v, want dynamic password bytes", config.Users)
	}
}
