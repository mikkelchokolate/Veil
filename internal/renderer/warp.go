package renderer

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/routing"
)

type WarpSingBoxConfig struct {
	Endpoint      string
	PrivateKey    string
	LocalAddress  string
	PeerPublicKey string
	Reserved      []int
	SocksListen   string
	SocksPort     int
	MTU           int
	RoutingRules  []WarpRoutingRule
}

type WarpRoutingRule struct {
	Match    string
	Outbound string
}

func RenderWarpSingBox(cfg WarpSingBoxConfig) (string, error) {
	if cfg.PrivateKey == "" {
		return "", errors.New("WARP private key is required")
	}
	if cfg.LocalAddress == "" {
		return "", errors.New("WARP local address is required")
	}
	if cfg.PeerPublicKey == "" {
		return "", errors.New("WARP peer public key is required")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "engage.cloudflareclient.com:2408"
	}
	if cfg.SocksListen == "" {
		cfg.SocksListen = "127.0.0.1"
	}
	if cfg.SocksPort == 0 {
		cfg.SocksPort = 40000
	}
	if cfg.SocksPort < 1 || cfg.SocksPort > 65535 {
		return "", errors.New("WARP SOCKS port must be between 1 and 65535")
	}
	if cfg.MTU == 0 {
		cfg.MTU = 1280
	}
	host, portText, err := net.SplitHostPort(cfg.Endpoint)
	if err != nil {
		return "", fmt.Errorf("WARP endpoint must be host:port: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("WARP endpoint port must be between 1 and 65535")
	}
	localAddresses := splitCSV(cfg.LocalAddress)
	if len(localAddresses) == 0 {
		return "", errors.New("WARP local address is required")
	}
	// sing-box >= 1.11 models WireGuard as an "endpoint" (not an outbound), and
	// since 1.12 the inline geoip/geosite route rules were removed in favour of
	// ip_is_private and rule_set. Emit the modern schema.
	peer := map[string]any{
		"address":     host,
		"port":        port,
		"public_key":  cfg.PeerPublicKey,
		"allowed_ips": []string{"0.0.0.0/0", "::/0"},
	}
	if len(cfg.Reserved) > 0 {
		peer["reserved"] = cfg.Reserved
	}
	body := map[string]any{
		"log": map[string]any{"level": "info"},
		"inbounds": []map[string]any{
			{
				"type":        "socks",
				"tag":         "warp-socks",
				"listen":      cfg.SocksListen,
				"listen_port": cfg.SocksPort,
			},
		},
		"endpoints": []map[string]any{
			{
				"type":        "wireguard",
				"tag":         "warp",
				"mtu":         cfg.MTU,
				"address":     localAddresses,
				"private_key": cfg.PrivateKey,
				"peers":       []map[string]any{peer},
			},
		},
		"outbounds": []map[string]any{
			{
				"type": "direct",
				"tag":  "direct",
			},
		},
		"route": renderWarpRoute(cfg.RoutingRules),
	}
	encoded, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}

// renderWarpRoute builds the sing-box >= 1.12 route. Traffic defaults to the
// WARP endpoint (final="warp"); rules bypass or redirect specific traffic.
// geoip:private maps to ip_is_private, and country geoip/geosite matches become
// remote rule_set references (the inline geoip/geosite fields were removed).
func renderWarpRoute(rules []WarpRoutingRule) map[string]any {
	rendered := []map[string]any{}
	ruleSets := []map[string]any{}
	seen := map[string]bool{}
	final := "warp"
	for _, rule := range rules {
		if rule.Outbound == "" || rule.Match == "" {
			continue
		}
		matchers, err := routing.ParseMatch(rule.Match)
		if err != nil {
			continue
		}
		outbound := rule.Outbound
		for _, matcher := range matchers {
			if matcher.Kind == routing.MatchAll {
				final = outbound
				continue
			}
			item := map[string]any{"outbound": outbound}
			switch matcher.Kind {
			case routing.MatchPrivateIP:
				item["ip_is_private"] = true
			case routing.MatchGeoIP:
				tag := "geoip-" + matcher.Value
				item["rule_set"] = tag
				if !seen[tag] {
					seen[tag] = true
					ruleSets = append(ruleSets, geoRuleSet(tag, "geoip", matcher.Value))
				}
			case routing.MatchGeoSite:
				tag := "geosite-" + matcher.Value
				item["rule_set"] = tag
				if !seen[tag] {
					seen[tag] = true
					ruleSets = append(ruleSets, geoRuleSet(tag, "geosite", matcher.Value))
				}
			case routing.MatchDomain:
				item["domain"] = matcher.Value
			case routing.MatchDomainSuffix:
				item["domain_suffix"] = matcher.Value
			case routing.MatchDomainKeyword:
				item["domain_keyword"] = matcher.Value
			case routing.MatchDomainRegex:
				item["domain_regex"] = matcher.Value
			case routing.MatchIPCIDR:
				item["ip_cidr"] = []string{matcher.Value}
			default:
				continue
			}
			rendered = append(rendered, item)
		}
	}
	route := map[string]any{"final": final}
	if len(rendered) > 0 {
		route["rules"] = rendered
	}
	if len(ruleSets) > 0 {
		route["rule_set"] = ruleSets
	}
	return route
}

// geoRuleSet references the official SagerNet remote rule-sets, downloaded via
// the direct outbound so resolution does not depend on the WARP tunnel itself.
func geoRuleSet(tag, kind, code string) map[string]any {
	return map[string]any{
		"type":            "remote",
		"tag":             tag,
		"format":          "binary",
		"url":             fmt.Sprintf("https://raw.githubusercontent.com/SagerNet/sing-%s/rule-set/%s-%s.srs", kind, kind, code),
		"download_detour": "direct",
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
