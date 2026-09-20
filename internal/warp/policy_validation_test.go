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

// #358: sing-box emits an unauthenticated SOCKS listener on SocksListen — a
// non-loopback bind is an open proxy on the network.
func TestValidateRejectsNonLoopbackSocksListen(t *testing.T) {
	base := Config{SocksPort: 40000, MTU: 1280, Reserved: []int{1, 2, 3}}
	for _, listen := range []string{
		"0.0.0.0", "::", "192.168.1.10", "203.0.113.5", "10.0.0.2",
		"169.254.1.1", "fe80::1", "localhost", "example.com", "not-an-ip",
	} {
		cfg := base
		cfg.SocksListen = listen
		if err := Validate(cfg); err == nil {
			t.Errorf("Validate(SocksListen=%q) unexpectedly succeeded", listen)
		}
	}
}

func TestValidateAcceptsLoopbackSocksListen(t *testing.T) {
	base := Config{SocksPort: 40000, MTU: 1280, Reserved: []int{1, 2, 3}}
	for _, listen := range []string{"", "127.0.0.1", "127.0.0.53", "::1"} {
		cfg := base
		cfg.SocksListen = listen
		if err := Validate(cfg); err != nil {
			t.Errorf("Validate(SocksListen=%q) = %v, want nil", listen, err)
		}
	}
}

// #579: a malformed endpoint previously passed Validate and only failed at
// render/apply time.
func TestValidateRejectsMalformedEndpoint(t *testing.T) {
	base := Config{SocksPort: 40000, MTU: 1280, Reserved: []int{1, 2, 3}}
	for _, endpoint := range []string{
		"engage.cloudflareclient.com",   // no port
		"engage.cloudflareclient.com:",  // empty port
		"engage.cloudflareclient.com:0", // port out of range
		"engage.cloudflareclient.com:65536",
		"engage.cloudflareclient.com:abc",
	} {
		cfg := base
		cfg.Endpoint = endpoint
		if err := Validate(cfg); err == nil {
			t.Errorf("Validate(Endpoint=%q) unexpectedly succeeded", endpoint)
		}
	}
}

func TestValidateAcceptsValidEndpoints(t *testing.T) {
	base := Config{SocksPort: 40000, MTU: 1280, Reserved: []int{1, 2, 3}}
	for _, endpoint := range []string{
		"",
		"engage.cloudflareclient.com:2408",
		"162.159.192.1:2408",
		"[2606:4700:d0::a29f:c001]:2408",
	} {
		cfg := base
		cfg.Endpoint = endpoint
		if err := Validate(cfg); err != nil {
			t.Errorf("Validate(Endpoint=%q) = %v, want nil", endpoint, err)
		}
	}
}
