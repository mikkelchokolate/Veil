package renderer

import (
	"io/fs"
	"reflect"
	"testing"
)

// TestProbeEgressDNSMatchesRenderTimeProbe is the #1288 doctor regression:
// `veil doctor` reports what the next apply renders, so ProbeEgressDNS must
// agree with filteredHostDoHAddr on every host shape, and it must say which
// nameservers it judged.
func TestProbeEgressDNSMatchesRenderTimeProbe(t *testing.T) {
	cases := []struct {
		name        string
		conf        string
		confErr     error
		v4, v6      string
		nameservers []string
		reachable   bool
		missing     bool
		readErr     bool
		pinned      string
	}{
		{name: "resolved stub", conf: "nameserver 127.0.0.53\noptions edns0\n", v4: testIPv4DefaultRoute, nameservers: []string{"127.0.0.53"}, reachable: true},
		{name: "public nameserver", conf: "nameserver 185.12.64.1\n", v4: testIPv4DefaultRoute, nameservers: []string{"185.12.64.1"}, reachable: true},
		{name: "one reachable among denied", conf: "nameserver 10.0.0.2\nnameserver 1.1.1.1\n", v4: testIPv4DefaultRoute, nameservers: []string{"10.0.0.2", "1.1.1.1"}, reachable: true},
		{name: "IPv4-mapped resolved stub", conf: "nameserver ::ffff:127.0.0.53\n", v4: testIPv4DefaultRoute, nameservers: []string{"::ffff:127.0.0.53"}, reachable: true},
		{name: "AWS VPC resolver", conf: "search ec2.internal\nnameserver 172.31.0.2\n", v4: testIPv4DefaultRoute, nameservers: []string{"172.31.0.2"}, pinned: filteredHostDoHAddrV4},
		{name: "local dnsmasq", conf: "nameserver 127.0.0.1\n", v4: testIPv4DefaultRoute, nameservers: []string{"127.0.0.1"}, pinned: filteredHostDoHAddrV4},
		{name: "zoned IPv6 link-local", conf: "nameserver fe80::1%eth0\n", v4: testIPv4DefaultRoute, nameservers: []string{"fe80::1%eth0"}, pinned: filteredHostDoHAddrV4},
		{name: "reachable nameserver past the third is ignored", conf: "nameserver 10.0.0.2\nnameserver 10.0.0.3\nnameserver 172.31.0.2\nnameserver 1.1.1.1\n", v4: testIPv4DefaultRoute, nameservers: []string{"10.0.0.2", "10.0.0.3", "172.31.0.2"}, pinned: filteredHostDoHAddrV4},
		{name: "unparseable nameserver does not count toward three", conf: "nameserver bogus\nnameserver 10.0.0.2\nnameserver 10.0.0.3\nnameserver 1.1.1.1\n", v4: testIPv4DefaultRoute, nameservers: []string{"10.0.0.2", "10.0.0.3", "1.1.1.1"}, reachable: true},
		{name: "IPv6-only host denied nameserver", conf: "nameserver fd00::53\n", v4: testIPv4NoDefault, v6: testIPv6DefaultRoute, nameservers: []string{"fd00::53"}, pinned: filteredHostDoHAddrV6},
		{name: "empty resolv.conf", conf: "", v4: testIPv4DefaultRoute, pinned: filteredHostDoHAddrV4},
		{name: "no resolv.conf", confErr: fs.ErrNotExist, v4: testIPv4DefaultRoute, missing: true, pinned: filteredHostDoHAddrV4},
		{name: "unreadable resolv.conf", confErr: fs.ErrPermission, v4: testIPv4DefaultRoute, readErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEgressHostProbe(t, tc.conf, tc.confErr, tc.v4, tc.v6)
			got := ProbeEgressDNS()
			if !reflect.DeepEqual(got.Nameservers, tc.nameservers) {
				t.Fatalf("Nameservers = %q, want %q", got.Nameservers, tc.nameservers)
			}
			if got.Reachable != tc.reachable || got.ResolvConfMissing != tc.missing || (got.ReadError != "") != tc.readErr || got.PinnedDoH != tc.pinned {
				t.Fatalf("report = %+v, want reachable=%v missing=%v readErr=%v pinned=%q", got, tc.reachable, tc.missing, tc.readErr, tc.pinned)
			}
			// The doctor must never disagree with what the renderers pin.
			renderAddr, renderPins := filteredHostDoHAddr()
			if renderAddr != got.PinnedDoH || renderPins != (got.PinnedDoH != "") {
				t.Fatalf("render-time probe pins %q (%v), doctor report pins %q", renderAddr, renderPins, got.PinnedDoH)
			}
		})
	}
}

// TestProbeEgressDNSIgnoresTestBinaryGate: the render-time probe is off in
// test binaries unless a renderer test opts in, but the doctor probe is an
// explicit request and must read the (stubbed) host either way.
func TestProbeEgressDNSIgnoresTestBinaryGate(t *testing.T) {
	withEgressHostProbe(t, "nameserver 172.31.0.2\n", nil, testIPv4DefaultRoute, "")
	egressHostProbeInTests = false
	if _, ok := filteredHostDoHAddr(); ok {
		t.Fatal("render-time probe must stay off in test binaries without opt-in")
	}
	if got := ProbeEgressDNS(); got.Reachable || got.PinnedDoH != filteredHostDoHAddrV4 {
		t.Fatalf("ProbeEgressDNS = %+v, want unreachable with pinned %s", got, filteredHostDoHAddrV4)
	}
}
