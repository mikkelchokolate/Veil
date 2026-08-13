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
	links, err := NewClientAccessProtocolRegistry().BuildAllLinks(settings, inbounds)
	if err != nil {
		return ClientLinksResponse{}, err
	}
	response.Links = append(response.Links, links...)
	return NewClientLinksResponseFinalizer().Finalize(response)
}

func NaiveClientURI(domain string, port int, username string, password string) string {
	userinfo := url.UserPassword(username, password).String()
	return fmt.Sprintf("naive+https://%s@%s:%d", userinfo, domain, port)
}

func Hysteria2ClientURI(domain string, port int, password string, name string, insecure bool) string {
	query := url.Values{}
	query.Set("sni", domain)
	if insecure {
		// Allow clients to skip verification when the server is using a
		// self-signed certificate instead of a publicly-trusted one.
		query.Set("insecure", "1")
	}
	fragment := url.QueryEscape(name)
	// Userinfo must be percent-encoded per RFC 3986. url.QueryEscape maps
	// space to '+', which hysteria clients do not translate back in the
	// userinfo component, silently breaking auth for passwords containing
	// spaces or other query-reserved characters (audit #71/#120).
	return fmt.Sprintf("hysteria2://%s@%s:%d/?%s#%s", escapeUserInfoComponent(password), domain, port, query.Encode(), fragment)
}

// escapeUserInfoComponent percent-encodes a single userinfo component
// (username or password) per RFC 3986, keeping only unreserved characters
// (A-Z a-z 0-9 - . _ ~). Everything else — including ':' which Go's url.Parse
// and hysteria clients treat as the user/password separator — is encoded.
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

// MieruClientURI builds mieru's "simple" share URI (mierus://), which the mieru
// client imports via `mieru import config`. Example:
// mierus://user:pass@host?port=3453&profile=name&protocol=UDP
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
