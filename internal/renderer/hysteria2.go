package renderer

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/routing"
	"gopkg.in/yaml.v3"
)

type Hysteria2User struct {
	Username string
	Password string
}

type Hysteria2Config struct {
	ListenPort    int
	Domain        string
	Password      string
	Users         []Hysteria2User
	MasqueradeURL string
	Upstream      string
	// CertPath/KeyPath point at a TLS cert/key Hysteria2 serves. Defaults to
	// Veil's managed self-signed panel cert, so Hysteria2 works on any host
	// (bare IP, no domain, ports 80/443 taken) without ACME — the client
	// connects with insecure/SNI. This is what makes "enter a domain and it
	// just works" hold for Hysteria2.
	CertPath           string
	KeyPath            string
	TrafficStatsListen string
	TrafficStatsSecret string
	// HTTPAuthURL, when non-empty, switches the daemon's auth mode to the
	// panel-internal HTTP callback (auth.type=http). The callback validates
	// credentials against the normalized client store and enforces
	// deviceLimit/ipLimit at session admission — the only place those limits
	// can actually be enforced (#1173). Rendered only when at least one
	// admitted runtime credential carries a connection limit.
	HTTPAuthURL string
	// RoutingRules split Hysteria2 traffic when Upstream (WARP) is set.
	// direct leaves locally (bypass proxy and WARP). warp uses Upstream.
	// proxy uses the protocol's own exit, not WARP.
	RoutingRules []Hysteria2RoutingRule
	GeoIPPath    string
	GeoSitePath  string
}

type Hysteria2RoutingRule struct {
	Match    string
	Outbound string
}

type hysteria2YAML struct {
	Listen string `yaml:"listen"`
	TLS    struct {
		Cert string `yaml:"cert"`
		Key  string `yaml:"key"`
	} `yaml:"tls"`
	Auth struct {
		Type     string                 `yaml:"type"`
		Password string                 `yaml:"password,omitempty"`
		UserPass map[string]string      `yaml:"userpass,omitempty"`
		HTTP     *hysteria2HTTPAuthYAML `yaml:"http,omitempty"`
	} `yaml:"auth"`
	Masquerade struct {
		Type  string `yaml:"type"`
		Proxy struct {
			URL         string `yaml:"url"`
			RewriteHost bool   `yaml:"rewriteHost"`
		} `yaml:"proxy"`
	} `yaml:"masquerade"`
	Outbounds    []hysteria2OutboundYAML `yaml:"outbounds,omitempty"`
	ACL          *hysteria2ACLYAML       `yaml:"acl,omitempty"`
	TrafficStats *struct {
		Listen string `yaml:"listen"`
		Secret string `yaml:"secret"`
	} `yaml:"trafficStats,omitempty"`
	// speedTest lets the official Hysteria client measure the QUIC path
	// directly (not Ookla through proxied TCP).
	SpeedTest bool `yaml:"speedTest"`
	// ignoreClientBandwidth disables Hysteria Brutal CC using the client's
	// advertised rates. GUI clients (Throne, NekoBox) often send wrong
	// bandwidth and the QUIC path then stalls ("pauses") under load.
	IgnoreClientBandwidth bool               `yaml:"ignoreClientBandwidth"`
	QUIC                  *hysteria2QUICYAML `yaml:"quic,omitempty"`
}

// hysteria2HTTPAuthYAML mirrors apernet/hysteria serverConfigAuthHTTP:
// auth.http.url is the endpoint the daemon POSTs {addr, auth, tx} to on every
// session admission; insecure only applies to https URLs and stays false.
type hysteria2HTTPAuthYAML struct {
	URL      string `yaml:"url"`
	Insecure bool   `yaml:"insecure"`
}

type hysteria2QUICYAML struct {
	MaxIdleTimeout  string `yaml:"maxIdleTimeout,omitempty"`
	KeepAlivePeriod string `yaml:"keepAlivePeriod,omitempty"`
}

type hysteria2OutboundYAML struct {
	Name   string `yaml:"name"`
	Type   string `yaml:"type"`
	Socks5 *struct {
		Addr string `yaml:"addr"`
	} `yaml:"socks5,omitempty"`
}

type hysteria2ACLYAML struct {
	Inline  []string `yaml:"inline,omitempty"`
	GeoIP   string   `yaml:"geoip,omitempty"`
	GeoSite string   `yaml:"geosite,omitempty"`
}

func RenderHysteria2(cfg Hysteria2Config) (string, error) {
	if cfg.ListenPort <= 0 {
		return "", errors.New("listen port is required")
	}
	if len(cfg.Users) == 0 {
		if cfg.Password == "" {
			return "", errors.New("password is required")
		}
	} else {
		for _, user := range cfg.Users {
			if user.Username == "" || user.Password == "" {
				return "", errors.New("username and password are required")
			}
		}
	}
	if cfg.MasqueradeURL == "" {
		cfg.MasqueradeURL = "https://www.bing.com/"
	}
	if cfg.CertPath == "" {
		cfg.CertPath = filepath.Join(hostenv.EtcDir(), "panel", "tls.crt")
	}
	if cfg.KeyPath == "" {
		cfg.KeyPath = filepath.Join(hostenv.EtcDir(), "panel", "tls.key")
	}

	var doc hysteria2YAML
	doc.Listen = ":" + itoa(cfg.ListenPort)
	doc.TLS.Cert = cfg.CertPath
	doc.TLS.Key = cfg.KeyPath
	switch {
	case cfg.HTTPAuthURL != "":
		// HTTP auth replaces both password and userpass: the callback does
		// the same credential check plus the connection-limit admission the
		// static modes cannot express (#1173).
		doc.Auth.Type = "http"
		doc.Auth.HTTP = &hysteria2HTTPAuthYAML{URL: cfg.HTTPAuthURL}
	case len(cfg.Users) > 0:
		doc.Auth.Type = "userpass"
		doc.Auth.UserPass = make(map[string]string, len(cfg.Users))
		for _, u := range cfg.Users {
			doc.Auth.UserPass[u.Username] = u.Password
		}
	default:
		doc.Auth.Type = "password"
		doc.Auth.Password = cfg.Password
	}
	doc.Masquerade.Type = "proxy"
	doc.Masquerade.Proxy.URL = cfg.MasqueradeURL
	doc.Masquerade.Proxy.RewriteHost = true
	doc.SpeedTest = true
	doc.IgnoreClientBandwidth = true
	doc.QUIC = &hysteria2QUICYAML{
		MaxIdleTimeout:  "2m",
		KeepAlivePeriod: "10s",
	}
	// The egress ACL is ALWAYS emitted — with or without a WARP upstream —
	// because without it Hysteria2 proxies client requests to ANY destination
	// the server can reach, including loopback control planes (Caddy admin on
	// 127.0.0.1:2019, the panel backend, DNS stubs) and private/link-local
	// infrastructure (#1095). The leading reject() rules are evaluated before
	// every operator routing rule, so no user rule can reopen them; Hysteria's
	// ACL engine matches resolved IPv4/IPv6 too, so a domain name that resolves
	// into a denied range is rejected the same as a literal IP.
	acl := renderHysteria2ACL(cfg)
	doc.Outbounds = append(doc.Outbounds,
		hysteria2OutboundYAML{Name: "direct", Type: "direct"},
		hysteria2OutboundYAML{Name: "proxy", Type: "direct"},
	)
	if cfg.Upstream != "" {
		socks := hysteria2OutboundYAML{Name: "warp", Type: "socks5"}
		socks.Socks5 = &struct {
			Addr string `yaml:"addr"`
		}{Addr: cfg.Upstream}
		doc.Outbounds = append(doc.Outbounds, socks)
	}
	doc.ACL = &hysteria2ACLYAML{Inline: acl}
	// GeoIPPath/GeoSitePath carry the LIVE rules/*.dat locations; the protocol
	// layer only sets them when the artifact will exist post-promotion
	// (#1132). The values are trusted as configured — stat()ing them here
	// would race promotion, which copies the staged files to the live root
	// after this render.
	doc.ACL.GeoIP = cfg.GeoIPPath
	doc.ACL.GeoSite = cfg.GeoSitePath

	if cfg.TrafficStatsListen != "" || cfg.TrafficStatsSecret != "" {
		if cfg.TrafficStatsListen == "" || cfg.TrafficStatsSecret == "" {
			return "", errors.New("traffic stats listen and secret must be configured together")
		}
		doc.TrafficStats = &struct {
			Listen string `yaml:"listen"`
			Secret string `yaml:"secret"`
		}{Listen: cfg.TrafficStatsListen, Secret: cfg.TrafficStatsSecret}
	}

	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return out.String(), nil
}

func renderHysteria2ACL(cfg Hysteria2Config) []string {
	hasUpstream := cfg.Upstream != ""
	// geoip:/geosite: ACL lines need their database on disk; the caller only
	// sets the path when the artifact exists (or will exist post-promotion).
	hasGeoIP := cfg.GeoIPPath != ""
	hasGeoSite := cfg.GeoSitePath != ""
	lines := []string{}
	// Non-overridable rejects first: every restricted destination class is
	// refused before any operator rule is consulted (#1095).
	for _, cidr := range egressDenyCIDRs {
		lines = append(lines, "reject("+cidr+")")
	}
	final := "direct"
	if hasUpstream {
		final = "warp"
	}
	for _, rule := range cfg.RoutingRules {
		if rule.Match == "" || rule.Outbound == "" {
			continue
		}
		matchers, err := routing.ParseMatch(rule.Match)
		if err != nil {
			continue
		}
		outbound := hysteria2ACLOutbound(rule.Outbound, hasUpstream)
		for _, matcher := range matchers {
			if matcher.Kind == routing.MatchAll {
				final = outbound
				continue
			}
			line, ok := hysteria2ACLLine(outbound, matcher, hasGeoIP, hasGeoSite)
			if ok {
				lines = append(lines, line)
			}
		}
	}
	return append(lines, final+"(all)")
}

// hysteria2ACLOutbound maps an operator routing outbound name onto an outbound
// the rendered config actually defines. "warp" only exists when a WARP
// upstream is configured; without it the safest equivalent is "direct" —
// never the missing name, which would fail ACL compilation at config load.
func hysteria2ACLOutbound(outbound string, hasUpstream bool) string {
	switch strings.ToLower(strings.TrimSpace(outbound)) {
	case "direct":
		return "direct"
	case "proxy":
		return "proxy"
	case "warp":
		if hasUpstream {
			return "warp"
		}
		return "direct"
	default:
		if hasUpstream {
			return "warp"
		}
		return "direct"
	}
}

// hysteria2ACLLine renders one `outbound(address)` ACL line using only
// address forms apernet/hysteria implements (see routing.Hysteria2ACLAddress).
// Matchers with no Hysteria dialect — keyword/regexp, or values carrying
// grammar-breaking characters like `#` — report ok=false rather than emit a
// fake prefix that Hysteria would miscompile or reject at config load (#679);
// the apply plan surfaces the dropped atoms as a warning issue.
func hysteria2ACLLine(outbound string, matcher routing.Matcher, hasGeoIP, hasGeoSite bool) (string, bool) {
	switch matcher.Kind {
	case routing.MatchPrivateIP, routing.MatchGeoIP:
		// geoip: addresses need the GeoIP database; without it the line
		// would crash Hysteria2 at load, so the atom is dropped instead.
		if !hasGeoIP {
			return "", false
		}
	case routing.MatchGeoSite:
		if !hasGeoSite {
			return "", false
		}
	}
	address, ok := routing.Hysteria2ACLAddress(matcher)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s(%s)", outbound, address), true
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
