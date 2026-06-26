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

func Hysteria2ClientURI(domain string, port int, password string, name string) string {
	query := url.Values{}
	query.Set("sni", domain)
	// The server serves a self-signed cert, so the client must skip cert
	// verification (pinning by SNI). This is what lets Hysteria2 work without
	// a publicly-trusted (ACME) certificate.
	query.Set("insecure", "1")
	fragment := url.QueryEscape(name)
	return fmt.Sprintf("hysteria2://%s@%s:%d/?%s#%s", url.QueryEscape(password), domain, port, query.Encode(), fragment)
}

func Hysteria2UserPassClientURI(domain string, port int, username string, password string, name string) string {
	query := url.Values{}
	query.Set("sni", domain)
	// Self-signed server cert: client skips verification (see above).
	query.Set("insecure", "1")
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
