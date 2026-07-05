package renderer

import (
	"encoding/json"
	"net"
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

func RenderCaddyJSON(plan caddyassembly.CaddyRenderPlan, caps caddycapabilities.CaddyCapabilities) ([]byte, error) {
	cfg := caddyConfig{Apps: map[string]any{}}
	cfg.Apps["http"] = renderHTTPApp(plan, caps)
	cfg.Apps["tls"] = renderTLSApp(plan)
	return json.MarshalIndent(cfg, "", "  ")
}

type caddyConfig struct {
	Apps map[string]any `json:"apps"`
}

func renderHTTPApp(plan caddyassembly.CaddyRenderPlan, caps caddycapabilities.CaddyCapabilities) map[string]any {
	servers := make(map[string]any)
	for key, owner := range plan.Servers {
		serverName := serverNameFor(key)
		servers[serverName] = renderServer(key, owner, caps)
	}
	for key, owner := range plan.ACMEChallenges {
		serverName := serverNameFor(key) + "-acme"
		servers[serverName] = renderAcmeChallengeServer(key, owner)
	}
	return map[string]any{"servers": servers}
}

func renderTLSApp(plan caddyassembly.CaddyRenderPlan) map[string]any {
	type issuerGroup struct {
		email   string
		mode    string
		domains []string
	}
	groups := make(map[string]*issuerGroup)
	for _, spec := range plan.Domains {
		mode := challengeForDomain(plan, spec.Domain)
		issuerKey := spec.Email + "/" + mode
		g := groups[issuerKey]
		if g == nil {
			g = &issuerGroup{email: spec.Email, mode: mode}
			groups[issuerKey] = g
		}
		g.domains = append(g.domains, spec.Domain)
	}
	var policies []map[string]any
	for _, g := range groups {
		policies = append(policies, map[string]any{
			"subjects": g.domains,
			"issuers":  []map[string]any{renderACMEIssuer(g.email, g.mode)},
		})
	}
	return map[string]any{"automation": map[string]any{"policies": policies}}
}

func renderACMEIssuer(email, mode string) map[string]any {
	return map[string]any{
		"module": "acme",
		"email":  email,
		"challenges": map[string]any{
			"http":     map[string]any{"disabled": mode != "http-01"},
			"tls-alpn": map[string]any{"disabled": mode != "tls-alpn-01"},
		},
	}
}

func renderServer(key bindregistry.BindKey, owner caddyassembly.CaddyBindOwner, caps caddycapabilities.CaddyCapabilities) map[string]any {
	server := map[string]any{
		"listen":     []string{listenString(key)},
		"auto_https": map[string]any{"disable_redirects": true},
	}
	switch owner.Kind {
	case caddyassembly.CaddyOwnerPanel:
		server["routes"] = []map[string]any{
			{
				"match": []map[string]any{{"host": []string{owner.Domain}}},
				"handle": []map[string]any{{
					"handler":   "reverse_proxy",
					"upstreams": []map[string]any{{"dial": "127.0.0.1:8080"}},
				}},
			},
			{
				"handle": []map[string]any{{"handler": "static_response", "status_code": 404}},
			},
		}
	case caddyassembly.CaddyOwnerNaive:
		handlers := []map[string]any{
			{
				"handler":          "forward_proxy",
				"basic_auth":       []map[string]any{}, // populated from inbound profiles in Task 15
				"hide_ip":          true,
				"hide_via":         true,
				"probe_resistance": map[string]any{},
			},
			{
				"handler": "file_server",
				"root":    "/var/lib/veil/www",
			},
		}
		server["routes"] = []map[string]any{{"handle": handlers}}
	}
	return server
}

func renderAcmeChallengeServer(key bindregistry.BindKey, owner caddyassembly.AcmeChallengeOwner) map[string]any {
	return map[string]any{
		"listen":     []string{listenString(key)},
		"auto_https": map[string]any{"disable_redirects": true},
		"routes":     []map[string]any{},
	}
}

func serverNameFor(key bindregistry.BindKey) string {
	return string(key.Network) + "-" + key.Address + "-" + portString(key.Port)
}

func listenString(key bindregistry.BindKey) string {
	if bindregistry.IsWildcard(key.Address) {
		return ":" + portString(key.Port)
	}
	return net.JoinHostPort(key.Address, portString(key.Port))
}

func portString(p int) string { return strconv.Itoa(p) }

func challengeForDomain(plan caddyassembly.CaddyRenderPlan, domain string) string {
	for _, owner := range plan.ACMEChallenges {
		for _, d := range owner.Domains {
			if d == domain {
				return owner.ChallengeMode
			}
		}
	}
	return "tls-alpn-01"
}
