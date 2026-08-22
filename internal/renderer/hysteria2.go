package renderer

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

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
		Type     string            `yaml:"type"`
		Password string            `yaml:"password,omitempty"`
		UserPass map[string]string `yaml:"userpass,omitempty"`
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
		cfg.CertPath = "/etc/veil/panel/tls.crt"
	}
	if cfg.KeyPath == "" {
		cfg.KeyPath = "/etc/veil/panel/tls.key"
	}

	var doc hysteria2YAML
	doc.Listen = ":" + itoa(cfg.ListenPort)
	doc.TLS.Cert = cfg.CertPath
	doc.TLS.Key = cfg.KeyPath
	if len(cfg.Users) > 0 {
		doc.Auth.Type = "userpass"
		doc.Auth.UserPass = make(map[string]string, len(cfg.Users))
		for _, u := range cfg.Users {
			doc.Auth.UserPass[u.Username] = u.Password
		}
	} else {
		doc.Auth.Type = "password"
		doc.Auth.Password = cfg.Password
	}
	doc.Masquerade.Type = "proxy"
	doc.Masquerade.Proxy.URL = cfg.MasqueradeURL
	doc.Masquerade.Proxy.RewriteHost = true
	doc.SpeedTest = true
	if cfg.Upstream != "" {
		acl := renderHysteria2ACL(cfg)
		if len(acl) > 0 {
			doc.Outbounds = append(doc.Outbounds,
				hysteria2OutboundYAML{Name: "direct", Type: "direct"},
				hysteria2OutboundYAML{Name: "proxy", Type: "direct"},
			)
		}
		socks := hysteria2OutboundYAML{Name: "warp", Type: "socks5"}
		socks.Socks5 = &struct {
			Addr string `yaml:"addr"`
		}{Addr: cfg.Upstream}
		doc.Outbounds = append(doc.Outbounds, socks)
		if len(acl) > 0 {
			doc.ACL = &hysteria2ACLYAML{Inline: acl}
			if path := usableRoutingDat(cfg.GeoIPPath); path != "" {
				doc.ACL.GeoIP = path
			}
			if path := usableRoutingDat(cfg.GeoSitePath); path != "" {
				doc.ACL.GeoSite = path
			}
		}
	}

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
	hasGeoIP := usableRoutingDat(cfg.GeoIPPath) != ""
	hasGeoSite := usableRoutingDat(cfg.GeoSitePath) != ""
	lines := []string{}
	final := "warp"
	for _, rule := range cfg.RoutingRules {
		if rule.Match == "" || rule.Outbound == "" {
			continue
		}
		matchers, err := routing.ParseMatch(rule.Match)
		if err != nil {
			continue
		}
		outbound := hysteria2ACLOutbound(rule.Outbound)
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
	if len(lines) == 0 {
		return nil
	}
	return append(lines, final+"(all)")
}

func hysteria2ACLOutbound(outbound string) string {
	switch strings.ToLower(strings.TrimSpace(outbound)) {
	case "direct":
		return "direct"
	case "proxy":
		return "proxy"
	case "warp":
		return "warp"
	default:
		return "warp"
	}
}

func hysteria2ACLLine(outbound string, matcher routing.Matcher, hasGeoIP, hasGeoSite bool) (string, bool) {
	switch matcher.Kind {
	case routing.MatchPrivateIP:
		if !hasGeoIP {
			return "", false
		}
		return fmt.Sprintf("%s(geoip:private)", outbound), true
	case routing.MatchGeoIP:
		if !hasGeoIP {
			return "", false
		}
		return fmt.Sprintf("%s(geoip:%s)", outbound, matcher.Value), true
	case routing.MatchGeoSite:
		if !hasGeoSite {
			return "", false
		}
		return fmt.Sprintf("%s(geosite:%s)", outbound, matcher.Value), true
	case routing.MatchDomain:
		return fmt.Sprintf("%s(domain:%s)", outbound, matcher.Value), true
	case routing.MatchDomainSuffix:
		return fmt.Sprintf("%s(suffix:%s)", outbound, matcher.Value), true
	case routing.MatchDomainKeyword:
		return fmt.Sprintf("%s(keyword:%s)", outbound, matcher.Value), true
	case routing.MatchDomainRegex:
		return fmt.Sprintf("%s(regex:%s)", outbound, matcher.Value), true
	case routing.MatchIPCIDR:
		return fmt.Sprintf("%s(cidr:%s)", outbound, matcher.Value), true
	default:
		return "", false
	}
}

func usableRoutingDat(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return path
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
