package hysteria2

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func masqueradeInbound(raw string) model.Inbound {
	return model.Inbound{
		Name:              "hy2",
		Protocol:          "hysteria2",
		Transport:         "udp",
		Port:              443,
		Enabled:           true,
		MasqueradeURL:     raw,
		Hysteria2Password: "pw",
	}
}

// TestValidateInboundRejectsRestrictedMasqueradeTarget locks in #1292 F5:
// masquerade.proxy.url is a server-side reverse-proxy target, so a URL that
// points at loopback, private, link-local (cloud metadata), CGNAT, multicast,
// reserved or unspecified space would bridge any unauthenticated visitor to a
// host-local service. The kernel egress filter on veil-hysteria2@.service
// already blocks the dial; the validator mirrors that deny set so such a
// config cannot be persisted in the first place.
func TestValidateInboundRejectsRestrictedMasqueradeTarget(t *testing.T) {
	p := Plugin{}
	for _, bad := range []string{
		"http://127.42.0.1:2019/",          // Caddy admin API
		"http://127.0.0.1/",                // panel backend
		"http://127.1/",                    // inet_aton shorthand loopback
		"http://2130706433/",               // decimal loopback
		"http://0x7f000001/",               // hex loopback
		"http://0177.0.0.1/",               // octal loopback
		"http://localhost:8080/",           // loopback name
		"http://LOCALHOST./",               // loopback name, FQDN form
		"http://admin.localhost/",          // RFC 6761 .localhost
		"http://169.254.169.254/latest/",   // cloud metadata
		"http://10.0.0.5/",                 // RFC 1918
		"http://172.16.1.1/",               // RFC 1918
		"https://192.168.1.1/",             // RFC 1918
		"http://100.64.0.1/",               // CGNAT
		"http://0.0.0.0/",                  // unspecified
		"http://224.0.0.1/",                // multicast
		"http://240.0.0.1/",                // reserved
		"http://[::1]:2019/",               // IPv6 loopback
		"http://[::]/",                     // IPv6 unspecified
		"http://[::ffff:127.0.0.1]/",       // IPv4-mapped loopback
		"http://[::ffff:169.254.169.254]/", // IPv4-mapped metadata
		"http://[64:ff9b::7f00:1]/",        // NAT64-embedded loopback
		"http://[fd00::1]/",                // unique-local
		"http://[fe80::1%25eth0]/",         // link-local with zone
		"http://[ff02::1]/",                // multicast
		" http://127.0.0.1/ ",              // surrounding whitespace
	} {
		issues := p.ValidateInbound(model.Settings{Domain: "hy.example.com"}, masqueradeInbound(bad))
		got := issueByCode(issues, "hysteria2_masquerade_invalid")
		if got == nil {
			t.Errorf("masqueradeURL %q: expected hysteria2_masquerade_invalid, got %+v", bad, issues)
			continue
		}
		if !strings.Contains(got.Message, "public") {
			t.Errorf("masqueradeURL %q: message should explain the public-destination rule, got %q", bad, got.Message)
		}
	}
}

// TestValidateInboundAcceptsPublicMasqueradeTarget guards against an
// over-broad policy: public hostnames, public IP literals and hostnames that
// merely contain digits or a "localhost"-like label must still be accepted.
func TestValidateInboundAcceptsPublicMasqueradeTarget(t *testing.T) {
	p := Plugin{}
	for _, good := range []string{
		"https://www.bing.com/",
		"https://1.1.1.1/",
		"http://8.8.8.8:8080/path",
		"https://[2606:4700:4700::1111]/",
		"https://cdn1.example.com/",
		"https://localhost.example.com/",
		"https://example.com.:443/",
		"https://123.example.com/",
	} {
		issues := p.ValidateInbound(model.Settings{Domain: "hy.example.com"}, masqueradeInbound(good))
		if got := issueByCode(issues, "hysteria2_masquerade_invalid"); got != nil {
			t.Errorf("masqueradeURL %q rejected: %+v", good, got)
		}
	}
}
