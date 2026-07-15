package warp

import "testing"

func TestValidateAcceptsDefaultAndRegisteredValues(t *testing.T) {
	cfg := Config{Reserved: []int{1, 2, 3}}
	SetDefaults(&cfg)
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate(defaults): %v", err)
	}
}

func TestValidateRejectsInvalidNumericValues(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "low socks port", cfg: Config{SocksPort: -1, MTU: 1280}},
		{name: "high socks port", cfg: Config{SocksPort: 65536, MTU: 1280}},
		{name: "low mtu", cfg: Config{SocksPort: 40000, MTU: 575}},
		{name: "high mtu", cfg: Config{SocksPort: 40000, MTU: 9001}},
		{name: "reserved length", cfg: Config{SocksPort: 40000, MTU: 1280, Reserved: []int{1, 2}}},
		{name: "negative reserved", cfg: Config{SocksPort: 40000, MTU: 1280, Reserved: []int{1, -1, 3}}},
		{name: "large reserved", cfg: Config{SocksPort: 40000, MTU: 1280, Reserved: []int{1, 256, 3}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate(tt.cfg); err == nil {
				t.Fatalf("Validate(%+v) unexpectedly succeeded", tt.cfg)
			}
		})
	}
}
