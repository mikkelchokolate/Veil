package warp

import "testing"

// TestValidateRejectsNonLoopbackSocksListen is the #358 regression: the
// sing-box WARP inbound is an unauthenticated SOCKS listener, so a
// non-loopback socksListen would expose an open proxy to the network.
func TestValidateRejectsNonLoopbackSocksListen(t *testing.T) {
	base := Config{SocksPort: 40000, MTU: 1280}
	for _, listen := range []string{"0.0.0.0", "::", "203.0.113.10", "192.168.1.5", "socks.example.com", "127.0.0.1:40000"} {
		cfg := base
		cfg.SocksListen = listen
		if err := Validate(cfg); err == nil {
			t.Fatalf("Validate(socksListen=%q) unexpectedly succeeded", listen)
		}
	}
}

// TestValidateAcceptsLoopbackSocksListen pins the control case: loopback
// listeners (and an empty value, which defaults to 127.0.0.1) still
// validate.
func TestValidateAcceptsLoopbackSocksListen(t *testing.T) {
	base := Config{SocksPort: 40000, MTU: 1280}
	for _, listen := range []string{"", "127.0.0.1", "127.0.0.2", "::1"} {
		cfg := base
		cfg.SocksListen = listen
		if err := Validate(cfg); err != nil {
			t.Fatalf("Validate(socksListen=%q): %v", listen, err)
		}
	}
}
