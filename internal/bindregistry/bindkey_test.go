package bindregistry

import "testing"

func TestNormalizeAddress(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "0.0.0.0"},
		{name: "star", in: "*", want: "0.0.0.0"},
		{name: "trimmed ipv4", in: " 192.0.2.10 ", want: "192.0.2.10"},
		{name: "bracketed ipv6", in: "[2001:db8::1]", want: "2001:db8::1"},
		{name: "ipv4 mapped ipv6", in: "::ffff:192.0.2.10", want: "192.0.2.10"},
		{name: "ipv6 wildcard", in: "[::]", want: "::"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeAddress(tc.in); got != tc.want {
				t.Fatalf("NormalizeAddress(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBindKeyValidate(t *testing.T) {
	t.Parallel()

	valid := BindKey{Address: " 0.0.0.0 ", Port: 443, Network: "TCP"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}

	cases := []BindKey{
		{Address: "not-an-ip", Port: 443, Network: ListenTCP},
		{Address: "0.0.0.0", Port: 0, Network: ListenTCP},
		{Address: "0.0.0.0", Port: 65536, Network: ListenTCP},
		{Address: "0.0.0.0", Port: 443, Network: "sctp"},
	}
	for _, key := range cases {
		if err := key.Validate(); err == nil {
			t.Errorf("expected %v to be rejected", key)
		}
	}
}

func TestBindKeyOverlap(t *testing.T) {
	t.Parallel()

	tcp443 := BindKey{Address: "0.0.0.0", Port: 443, Network: ListenTCP}
	if !tcp443.Overlaps(BindKey{Address: "192.0.2.10", Port: 443, Network: ListenTCP}) {
		t.Error("IPv4 wildcard must overlap a specific IPv4 address")
	}
	if tcp443.Overlaps(BindKey{Address: "0.0.0.0", Port: 443, Network: ListenUDP}) {
		t.Error("TCP and UDP listeners must not overlap")
	}
	if tcp443.Overlaps(BindKey{Address: "0.0.0.0", Port: 8443, Network: ListenTCP}) {
		t.Error("different ports must not overlap")
	}
	if tcp443.Overlaps(BindKey{Address: "2001:db8::1", Port: 443, Network: ListenTCP}) {
		t.Error("IPv4 wildcard must not overlap a specific IPv6 address")
	}

	ipv6Wildcard := BindKey{Address: "::", Port: 443, Network: ListenTCP}
	if !ipv6Wildcard.Overlaps(tcp443) {
		t.Error("IPv6 wildcard is conservatively treated as dual-stack")
	}

	firstSpecific := BindKey{Address: "192.0.2.10", Port: 443, Network: ListenTCP}
	secondSpecific := BindKey{Address: "192.0.2.11", Port: 443, Network: ListenTCP}
	if firstSpecific.Overlaps(secondSpecific) {
		t.Error("different specific addresses must not overlap")
	}
}

func TestBindKeyCanonical(t *testing.T) {
	t.Parallel()

	got := (BindKey{Address: " [2001:db8::1] ", Port: 443, Network: " TCP "}).Canonical()
	want := BindKey{Address: "2001:db8::1", Port: 443, Network: ListenTCP}
	if got != want {
		t.Fatalf("Canonical() = %#v, want %#v", got, want)
	}
}
