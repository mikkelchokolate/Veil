package clientaccess

import (
	"fmt"
	"net"
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

// clientLinkEffectiveInbounds materializes protocol-specific dynamic password
// fields onto a copy for legacy client-access paths that still consume the flat
// Password field. The persisted desired state is never mutated.
func clientLinkEffectiveInbounds(inbounds []Inbound) []Inbound {
	out := append([]Inbound(nil), inbounds...)
	for i := range out {
		switch out[i].Protocol {
		case "olcrtc", "mieru":
			if password := protocolString(out[i].ProtocolFields, "password", ""); password != "" {
				out[i].Password = password
			}
		}
	}
	return out
}

func NaiveClientURI(domain string, port int, username string, password string) string {
	return naiveClientURITransport(domain, port, username, password, "https", 443)
}

// NaiveShareURI renders a GUI-importable NaiveProxy share URI
// (naive+https:// or naive+quic://). Launchers strip the naive+ prefix
// and pass the remainder to the naive binary as --proxy=.
func NaiveShareURI(domain string, port int, username, password, scheme string, defaultPort int) string {
	return naiveClientURITransport(domain, port, username, password, scheme, defaultPort)
}

func naiveShareScheme(scheme string) string {
	scheme = strings.TrimSpace(scheme)
	if scheme == "" {
		scheme = "https"
	}
	if strings.HasPrefix(scheme, "naive+") {
		return scheme
	}
	return "naive+" + scheme
}

// shareURIHost puts IPv6 literals in brackets so URI parsers do not treat
// the last hextet as a port. DNS names and IPv4 stay unbracketed.
func shareURIHost(host string) string {
	host = strings.TrimSpace(host)
	stripped := strings.Trim(host, "[]")
	if ip := net.ParseIP(stripped); ip != nil && ip.To4() == nil {
		return "[" + stripped + "]"
	}
	return host
}

func shareURIHostPort(host string, port int) string {
	host = strings.TrimSpace(host)
	return net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
}

// naiveClientURITransport renders a share-link URI for Clash/sing-box/v2rayN
// (naive+https:// / naive+quic://). The port is omitted when it equals the
// scheme default. Userinfo is percent-encoded via url.UserPassword.
func naiveClientURITransport(domain string, port int, username, password, scheme string, defaultPort int) string {
	userinfo := url.UserPassword(username, password).String()
	host := shareURIHost(domain)
	if port != defaultPort {
		host = shareURIHostPort(domain, port)
	}
	return fmt.Sprintf("%s://%s@%s", naiveShareScheme(scheme), userinfo, host)
}

func Hysteria2ClientURI(domain string, port int, password string, name string, insecure bool) string {
	query := url.Values{}
	query.Set("sni", domain)
	if insecure {
		query.Set("insecure", "1")
	}
	fragment := url.QueryEscape(name)
	return fmt.Sprintf("hysteria2://%s@%s/?%s#%s", escapeUserInfoComponent(password), shareURIHostPort(domain, port), query.Encode(), fragment)
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
	return fmt.Sprintf("hysteria2://%s@%s/?%s#%s", userinfo, shareURIHostPort(domain, port), query.Encode(), fragment)
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
	return fmt.Sprintf("mierus://%s@%s?%s", userinfo, shareURIHost(domain), query.Encode())
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
