package renderer

import (
	"os"
	"strings"
	"testing"
)

// TestMieruResolvConfOnlyOnDeniedHosts is the #1288 Mieru regression: mita
// has no resolver setting, so on a host whose nameservers the egress filter
// denies, veil-mieru must get a resolv.conf naming public nameservers of the
// family the host routes. Every other host (reachable nameserver, unreadable
// or missing resolv.conf) must get no file, so the unit keeps the host
// resolver.
func TestMieruResolvConfOnlyOnDeniedHosts(t *testing.T) {
	cases := []struct {
		name   string
		conf   string
		err    error
		v4, v6 string
		want   []string
	}{
		{name: "resolved stub", conf: "nameserver 127.0.0.53\n", v4: testIPv4DefaultRoute},
		{name: "public nameserver", conf: "nameserver 185.12.64.1\n", v4: testIPv4DefaultRoute},
		{name: "AWS VPC resolver", conf: "nameserver 172.31.0.2\n", v4: testIPv4DefaultRoute, want: []string{"nameserver 1.1.1.1\nnameserver 1.0.0.1\n"}},
		{name: "local dnsmasq", conf: "nameserver 127.0.0.1\n", v4: testIPv4DefaultRoute, want: []string{"nameserver 1.1.1.1\nnameserver 1.0.0.1\n"}},
		{name: "IPv6-only host", conf: "nameserver fd00::53\n", v4: testIPv4NoDefault, v6: testIPv6DefaultRoute, want: []string{"nameserver 2606:4700:4700::1111\nnameserver 2606:4700:4700::1001\n"}},
		{name: "missing resolv.conf", err: os.ErrNotExist, v4: testIPv4DefaultRoute},
		{name: "unreadable resolv.conf", err: os.ErrPermission, v4: testIPv4DefaultRoute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEgressHostProbe(t, tc.conf, tc.err, tc.v4, tc.v6)
			body, ok := MieruResolvConf(ProbeEgressDNS())
			if ok != (tc.want != nil) {
				t.Fatalf("MieruResolvConf ok = %v, want %v (body %q)", ok, tc.want != nil, body)
			}
			for _, want := range tc.want {
				if !strings.HasSuffix(string(body), want) {
					t.Fatalf("resolv.conf = %q, want it to end with %q", body, want)
				}
			}
			for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
				if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "nameserver ") {
					t.Fatalf("unexpected resolv.conf line %q in %q", line, body)
				}
			}
		})
	}
}

// TestMieruUnitBindsFallbackResolvConf pins the unit wiring: veil writes the
// file as root before mita starts and the bind tolerates its absence, in the
// full render, the custom-path drop-in and the packaged unit alike.
func TestMieruUnitBindsFallbackResolvConf(t *testing.T) {
	const bind = "BindReadOnlyPaths=-/run/veil-mieru/resolv.conf:/etc/resolv.conf\n"
	unit := RenderSystemdUnits(SystemdConfig{})[UnitMieru]
	packaged, err := os.ReadFile("../../packaging/systemd/veil-mieru.service")
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"rendered": unit, "packaged": strings.ReplaceAll(string(packaged), "\r\n", "\n")} {
		for _, want := range []string{
			"ExecStartPre=-+/usr/local/bin/veil egress-resolv-conf --output /run/veil-mieru/resolv.conf\n",
			bind,
			"RuntimeDirectory=veil-mieru\n",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s veil-mieru.service missing %q:\n%s", name, want, body)
			}
		}
		if strings.Index(body, "ExecStartPre=") > strings.Index(body, "ExecStart=") {
			t.Fatalf("%s veil-mieru.service: ExecStartPre must precede ExecStart:\n%s", name, body)
		}
	}
	if unit != strings.ReplaceAll(string(packaged), "\r\n", "\n") {
		t.Fatalf("rendered veil-mieru.service drifted from packaging/systemd/veil-mieru.service:\n--- rendered\n%s\n--- packaged\n%s", unit, packaged)
	}

	dropIn, ok := RenderInstallDropIn(UnitMieru, SystemdConfig{VeilBinary: "/opt/veil/bin/veil", EtcDir: "/opt/veil/etc"})
	if !ok {
		t.Fatal("expected a mieru drop-in for custom paths")
	}
	if !strings.Contains(dropIn, "ExecStartPre=\nExecStartPre=-+/opt/veil/bin/veil egress-resolv-conf --output /run/veil-mieru/resolv.conf\n") {
		t.Fatalf("mieru drop-in must re-point ExecStartPre at the custom veil binary:\n%s", dropIn)
	}
}
