package renderer

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
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
		"outbounds": []map[string]any{
			{
				"type":            "wireguard",
				"tag":             "warp",
				"server":          host,
				"server_port":     port,
				"local_address":   localAddresses,
				"private_key":     cfg.PrivateKey,
				"peer_public_key": cfg.PeerPublicKey,
				"reserved":        cfg.Reserved,
				"mtu":             cfg.MTU,
			},
			{
				"type": "direct",
				"tag":  "direct",
			},
		},
	}
	if route := renderWarpRoute(cfg.RoutingRules); route != nil {
		body["route"] = route
	}
	encoded, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}

func renderWarpRoute(rules []WarpRoutingRule) map[string]any {
	rendered := []map[string]any{}
	for _, rule := range rules {
		if rule.Outbound == "" || rule.Match == "" {
			continue
		}
		if rule.Match == "all" {
			return map[string]any{"rules": rendered, "final": rule.Outbound}
		}
		item := map[string]any{"outbound": rule.Outbound}
		switch {
		case strings.HasPrefix(rule.Match, "geoip:"):
			item["geoip"] = strings.TrimPrefix(rule.Match, "geoip:")
		case strings.HasPrefix(rule.Match, "geosite:"):
			item["geosite"] = strings.TrimPrefix(rule.Match, "geosite:")
		default:
			item["domain"] = rule.Match
		}
		rendered = append(rendered, item)
	}
	if len(rendered) == 0 {
		return nil
	}
	return map[string]any{"rules": rendered}
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
