package renderer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"sort"
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
			return fmt.Errorf("caddy binary does not include the forward_proxy module required for NaiveProxy")
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
	// plan.Domains is a map; sort its specs so both the per-policy subjects
	// slice and the policy list are byte-for-byte deterministic across renders.
	specs := make([]caddyassembly.CaddyDomainCertSpec, 0, len(plan.Domains))
	for _, spec := range plan.Domains {
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Domain < specs[j].Domain })
	for _, spec := range specs {
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
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		g := groups[k]
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
		"listen":                  []string{listenString(key)},
		"automatic_https":         map[string]any{"disable_redirects": true},
		"tls_connection_policies": []map[string]any{{}},
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
		server["routes"] = panelRoutes(owner.Domain, owner.BackendPort, owner.WebBasePath, true)
		server["errors"] = panelErrorRoutes()
	case caddyassembly.CaddyOwnerNaive:
		authCreds := make([]string, 0, len(owner.NaiveUsers))
		for _, user := range owner.NaiveUsers {
			// forwardproxy declares auth_credentials as [][]byte. Its Caddyfile
			// adapter stores each HTTP Basic value (already base64-encoded) as a
			// byte slice, so native JSON must base64-encode that byte slice once
			// more to satisfy encoding/json's []byte contract.
			basicValue := base64.StdEncoding.EncodeToString([]byte(user.Username + ":" + user.Password))
			authCreds = append(authCreds, base64.StdEncoding.EncodeToString([]byte(basicValue)))
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
			// On a shared Panel/Naive bind, unmatched requests must reach
			// forward_proxy. The panel-only catch-all 404 would intercept CONNECT.
			routes = append(routes, panelRoutes(owner.PanelDomain, owner.BackendPort, owner.WebBasePath, false)...)
		}
		proxyRoute := map[string]any{"handle": handlers}
		if domain := strings.TrimSpace(owner.Domain); domain != "" {
			// Caddy only manages certificates for domains it discovers from
			// route host matchers (or certificates.automate). Without a host
			// matcher the naive inbound domain never receives a certificate
			// and every TLS handshake fails (audit #122). Match the resolved
			// domain so automatic HTTPS provisions it; probe resistance is
			// preserved because forward_proxy still handles CONNECT on the
			// matched host and non-matching hosts fall through to the
			// catch-all file_server below.
			proxyRoute = map[string]any{
				"match":  []map[string]any{{"host": []string{domain}}},
				"handle": handlers,
			}
			routes = append(routes, proxyRoute)
			// Catch-all fallback keeps the site reachable for any other host
			// (probe resistance), while the matched route owns the certificate.
			routes = append(routes, map[string]any{"handle": []map[string]any{{"handler": "file_server", "root": fallbackRoot}}})
		} else {
			routes = append(routes, proxyRoute)
		}
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
// It must be a strict subdirectory of /var/lib/veil: serving exactly
// /var/lib/veil would expose state.json, audit/ and backups/ to anonymous
// naive-port visitors (audit #77 F1). Relative paths are resolved against
// /var/lib/veil.
func resolveNaiveFallbackRoot(input string) (string, error) {
	if input == "" {
		return "/var/lib/veil/www", nil
	}
	root := filepath.ToSlash(filepath.Clean(input))
	if !strings.HasPrefix(root, "/") {
		root = filepath.ToSlash(filepath.Clean("/var/lib/veil/" + root))
	}
	if root == "/var/lib/veil" || !strings.HasPrefix(root, "/var/lib/veil/") {
		return "", fmt.Errorf("fallback root must be a subdirectory of /var/lib/veil: %s", root)
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

func panelErrorRoutes() map[string]any {
	const body = `{"error":{"code":"gateway_error","message":"panel backend unavailable"}}`
	return map[string]any{
		"routes": []map[string]any{{
			"handle": []map[string]any{{
				"handler":     "static_response",
				"status_code": "{http.error.status_code}",
				"body":        body,
				"headers": map[string]any{
					"Content-Type":  []string{"application/json; charset=utf-8"},
					"Cache-Control": []string{"no-store"},
				},
			}},
		}},
	}
}

func panelRoutes(domain string, backendPort int, webBasePath string, includeFallback bool) []map[string]any {
	proxy := map[string]any{
		"handler":   "reverse_proxy",
		"upstreams": []map[string]any{{"dial": "127.0.0.1:" + portString(backendPort)}},
	}
	if webBasePath == "" || webBasePath == "/" {
		routes := []map[string]any{
			{"match": []map[string]any{{"host": []string{domain}}}, "handle": []map[string]any{proxy}},
		}
		if includeFallback {
			routes = append(routes, map[string]any{"handle": []map[string]any{{"handler": "static_response", "status_code": 404}}})
		}
		return routes
	}
	webBasePath = strings.TrimRight(webBasePath, "/")
	webBasePathSlash := webBasePath + "/"
	routes := []map[string]any{
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
	}
	if includeFallback {
		routes = append(routes, map[string]any{"handle": []map[string]any{{"handler": "static_response", "status_code": 404}}})
	}
	return routes
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
