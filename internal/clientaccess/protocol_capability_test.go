package clientaccess

import "testing"

func TestProtocolCapabilityCoverage(t *testing.T) {
	r := NewClientAccessProtocolRegistry()
	want := map[string]ProtocolCapability{
		"hysteria2":  {Protocol: "hysteria2", SupportsPerClientCredentials: true, SupportsFallback: true},
		"naiveproxy": {Protocol: "naiveproxy", SupportsPerClientCredentials: true, SupportsFallback: true},
		"olcrtc":     {Protocol: "olcrtc", SupportsPerClientCredentials: true, SupportsFallback: true},
		"mieru":      {Protocol: "mieru", SupportsPerClientCredentials: true, SupportsAggregation: true, SupportsFallback: true},
	}
	for name, w := range want {
		got, ok := r.Capability(name)
		if !ok {
			t.Fatalf("protocol %s missing from registry", name)
		}
		if got != w {
			t.Fatalf("capability %s = %+v, want %+v", name, got, w)
		}
	}
	if _, ok := r.Capability("nonexistent"); ok {
		t.Fatalf("unknown protocol must report no capability")
	}
	if got := r.Capabilities(); len(got) != 4 {
		t.Fatalf("Capabilities() len = %d, want 4", len(got))
	}
}
