package clientaccess

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// BuildClientLinks creates user-facing client connection links from settings and enabled inbounds.
// Per-inbound passwords override global settings passwords; empty per-inbound passwords fall back
// to the global protocol password for backward compatibility.
func BuildClientLinks(settings Settings, inbounds []Inbound) (ClientLinksResponse, error) {
	if err := NewClientLinksSettingsValidation().Validate(settings); err != nil {
		return ClientLinksResponse{}, err
	}
	response := NewClientLinksResponseMetadata(settings).Build()
	links, err := NewClientAccessProtocolRegistry().BuildAllLinks(settings, clientLinkEffectiveInbounds(inbounds))
	if err != nil {
		return ClientLinksResponse{}, err
	}
	response.Links = append(response.Links, links...)
	return NewClientLinksResponseFinalizer().Finalize(response)
}

// clientLinkEffectiveInbounds keeps the legacy client-access registry aligned
// with protocol-specific effective fields. olcRTC's dynamic form stores the
// shared crypto key in protocolFields.password; registry link builders consume
// the legacy flat Password field, so materialize the effective key on a copy
// before exporting links. The source desired state is never mutated.
func clientLinkEffectiveInbounds(inbounds []Inbound) []Inbound {
	out := append([]Inbound(nil), inbounds...)
	for i := range out {
		if out[i].Protocol != "olcrtc" {
			continue
		}
		if password := protocolString(out[i].ProtocolFields, "password", ""); password != "" {
			out[i].Password = password
		}
	}
	return out
}

func NaiveClientURI(domain string, port int, username string, password string) string {
	return naiveClientURITransport(domain, port, username, password, "https", 443)
}

// naiveClientURITransport renders the upstream naiveproxy share URI. The
// client (klzgrad/naiveproxy) accepts https:// (TCP/HTTP2) and quic://
// (HTTP/3/UDP); the port is omitted when it equals the scheme default
// (audit #177 — the legacy naive+https:// scheme is not understood by
// upstream clients). Userinfo is percent-encoded via url.UserPassword.
func naiveClientURITransport(domain string, port int, username, password, scheme string, defaultPort int) string {
	userinfo := url.UserPassword(username, password).String()
	host := domain
	if port != defaultPort {
		host = fmt.Sprintf("%s:%d", domain, port)
	}
	return fmt.Sprintf("%s://%s@%s", scheme, userinfo, host)
}

func Hysteria2ClientURI(domain string, port int, password string, name string, insecure bool) string {
	query := url.Values{}
	query.Set("sni", domain)
	if insecure {
		query.Set("insecure", "1")
	}
	fragment := url.QueryEscape(name)
	return fmt.Sprintf("hysteria2://%s@%s:%d/?%s#%s", escapeUserInfoComponent(password), domain, port, query.Encode(), fragment)
}

func escapeUserInfoComponent(value string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0x0f])
		}
	}
	return b.String()
}

func Hysteria2UserPassClientURI(domain string, port int, username string, password string, name string, insecure bool) string {
	query := url.Values{}
	query.Set("sni", domain)
	if insecure {
		query.Set("insecure", "1")
	}
	fragment := url.QueryEscape(name)
	userinfo := url.UserPassword(username, password).String()
	return fmt.Sprintf("hysteria2://%s@%s:%d/?%s#%s", userinfo, domain, port, query.Encode(), fragment)
}

func MieruClientURI(domain string, port int, username, password, profile, transport string) string {
	proto := strings.ToUpper(strings.TrimSpace(transport))
	if proto != "UDP" {
		proto = "TCP"
	}
	query := url.Values{}
	query.Set("port", strconv.Itoa(port))
	query.Set("profile", profile)
	query.Set("protocol", proto)
	userinfo := url.UserPassword(username, password).String()
	return fmt.Sprintf("mierus://%s@%s?%s", userinfo, domain, query.Encode())
}

func OlcrtcClientURI(auth, transport, roomID, key, mimo string) string {
	if auth == "" {
		auth = "jitsi"
	}
	if transport == "" {
		transport = "datachannel"
	}
	return fmt.Sprintf("olcrtc://%s?%s@%s#%s$%s", auth, transport, roomID, key, mimo)
}
