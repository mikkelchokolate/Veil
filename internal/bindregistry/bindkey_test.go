package bindregistry

import "testing"

func TestNormalizeAddress(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"0.0.0.0", "0.0.0.0"},
		{"", "0.0.0.0"},
		{"::", "::"},
		{" 0.0.0.0 ", "0.0.0.0"},
	}
	for _, c := range cases {
		got := NormalizeAddress(c.in)
		if got != c.want {
			t.Errorf("NormalizeAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBindKeyOverlap(t *testing.T) {
	wildcardTCP443 := BindKey{Address: "0.0.0.0", Port: 443, Network: ListenTCP}
	specificTCP443 := BindKey{Address: "192.168.1.10", Port: 443, Network: ListenTCP}
	if !wildcardTCP443.Overlaps(specificTCP443) {
		t.Error("wildcard IPv4 must overlap specific IPv4 on same port/protocol")
	}
	udp443 := BindKey{Address: "0.0.0.0", Port: 443, Network: ListenUDP}
	if wildcardTCP443.Overlaps(udp443) {
		t.Error("TCP and UDP on same port must not conflict")
	}
	otherPort := BindKey{Address: "192.168.1.10", Port: 8443, Network: ListenTCP}
	if specificTCP443.Overlaps(otherPort) {
		t.Error("different ports must not conflict")
	}
}
