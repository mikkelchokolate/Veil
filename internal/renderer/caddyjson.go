package renderer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

func RenderCaddyJSON(plan caddyassembly.CaddyRenderPlan, caps caddycapabilities.CaddyCapabilities) ([]byte, error) {
	if err := validateCaddyCapabilities(plan, caps); err != nil {
		return nil, err
	}
	httpApp, err := renderHTTPApp(plan, caps)
	if err != nil {
		return nil, err
	}
	cfg := caddyConfig{Admin: map[string]any{"listen": "127.0.0.1:2019"}, Apps: map[string]any{}}
	cfg.Apps["http"] = httpApp
	cfg.Apps["tls"] = renderTLSApp(plan)
	return json.MarshalIndent(cfg, "", "  ")
}

func validateCaddyCapabilities(plan caddyassembly.CaddyRenderPlan, caps caddycapabilities.CaddyCapabilities) error {
	for _, owner := range plan.Servers {
		if owner.Kind != caddyassembly.CaddyOwnerNaive {
			continue
		}
		if !caps.ForwardProxy {
			return fmt.Errorf("Caddy binary does not include the forward_proxy module required for NaiveProxy")
		}
	}
	return nil
}

type caddyConfig struct {
	Admin map[string]any `json:"admin"`
	Apps  map[string]any `json:"apps"`
}

func renderHTTPApp(plan caddyassembly.CaddyRenderPlan, caps caddycapabilities.CaddyCapabilities) (map[string]any, error) {
	servers := make(map[string]any)
	for key, owner := range plan.Servers {
		serverName := serverNameFor(key)
		server, err := renderServer(key, owner, caps)
		if err != nil {
			return nil, err
		}
		servers[serverName] = server
	}
	for key, owner := range plan.ACMEChallenges {
		serverName := serverNameFor(key) + "-acme"
		servers[serverName] = renderAcmeChallengeServer(key, owner)
	}
	return map[string]any{"servers": servers}, nil
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

func renderServer(key bindregistry.BindKey, owner caddyassembly.CaddyBindOwner, caps caddycapabilities.CaddyCapabilities) (map[string]any, error) {
	server := map[string]any{
		"listen":          []string{listenString(key)},
		"automatic_https": map[string]any{"disable_redirects": true},
	}
	if owner.Kind == caddyassembly.CaddyOwnerNaive {
		protocols, err := protocolsForTransport(owner.Transport)
		if err != nil {
			return nil, err
		}
		server["protocols"] = protocols
	}
	switch owner.Kind {
	case caddyassembly.CaddyOwnerPanel:
		server["routes"] = panelRoutes(owner.Domain, owner.BackendPort, owner.WebBasePath)
	case caddyassembly.CaddyOwnerNaive:
		authCreds := make([]string, 0, len(owner.NaiveUsers))
		for _, u := range owner.NaiveUsers {
			authCreds = append(authCreds, base64.StdEncoding.EncodeToString([]byte(u.Username+":"+u.Password)))
		}
		fallbackRoot, err := resolveNaiveFallbackRoot(owner.FallbackRoot)
		if err != nil {
			return nil, err
		}
		handlers := []map[string]any{
			{
				"handler":          "forward_proxy",
				"auth_credentials": authCreds,
				"hide_ip":          true,
				"hide_via":         true,
				"probe_resistance": map[string]any{},
			},
			{
				"handler": "file_server",
				"root":    fallbackRoot,
			},
		}
		routes := []map[string]any{}
		if owner.PanelDomain != "" && owner.BackendPort > 0 && owner.WebBasePath != "" {
			routes = append(routes, panelRoutes(owner.PanelDomain, owner.BackendPort, owner.WebBasePath)...)
		}
		routes = append(routes, map[string]any{"handle": handlers})
		server["routes"] = routes
	}
	return server, nil
}

func protocolsForTransport(transport string) ([]string, error) {
	switch transport {
	case "tcp":
		return []string{"h1", "h2"}, nil
	default:
		return nil, fmt.Errorf("unsupported naive transport %q", transport)
	}
}

// resolveNaiveFallbackRoot validates and normalizes the naive fallback root.
// It must be either exactly /var/lib/veil or a subdirectory of it.
// Relative paths are resolved against /var/lib/veil.
func resolveNaiveFallbackRoot(input string) (string, error) {
	if input == "" {
		return "/var/lib/veil/www", nil
	}
	root := filepath.ToSlash(filepath.Clean(input))
	if !strings.HasPrefix(root, "/") {
		root = filepath.ToSlash(filepath.Clean("/var/lib/veil/" + root))
	}
	if root != "/var/lib/veil" && !strings.HasPrefix(root, "/var/lib/veil/") {
		return "", fmt.Errorf("fallback root must be within /var/lib/veil: %s", root)
	}
	return root, nil
}

func renderAcmeChallengeServer(key bindregistry.BindKey, owner caddyassembly.AcmeChallengeOwner) map[string]any {
	return map[string]any{
		"listen":          []string{listenString(key)},
		"automatic_https": map[string]any{"disable_redirects": true},
		"routes":          []map[string]any{},
	}
}

func panelRoutes(domain string, backendPort int, webBasePath string) []map[string]any {
	proxy := map[string]any{
		"handler":   "reverse_proxy",
		"upstreams": []map[string]any{{"dial": "127.0.0.1:" + portString(backendPort)}},
	}
	if webBasePath == "" || webBasePath == "/" {
		return []map[string]any{
			{"match": []map[string]any{{"host": []string{domain}}}, "handle": []map[string]any{proxy}},
			{"handle": []map[string]any{{"handler": "static_response", "status_code": 404}}},
		}
	}
	webBasePath = strings.TrimRight(webBasePath, "/")
	webBasePathSlash := webBasePath + "/"
	return []map[string]any{
		{
			"match": []map[string]any{{"host": []string{domain}, "path": []string{webBasePath}}},
			"handle": []map[string]any{{
				"handler":     "static_response",
				"headers":     map[string]any{"Location": []string{webBasePathSlash}},
				"status_code": 308,
			}},
		},
		{
			"match":  []map[string]any{{"host": []string{domain}, "path": []string{webBasePathSlash + "*"}}},
			"handle": []map[string]any{proxy},
		},
		{
			"handle": []map[string]any{{"handler": "static_response", "status_code": 404}},
		},
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
	if plan.DefaultChallengeMode != "" {
		return plan.DefaultChallengeMode
	}
	return "tls-alpn-01"
}
