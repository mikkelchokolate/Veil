package renderer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

func RenderCaddyJSON(plan caddyassembly.CaddyRenderPlan, caps caddycapabilities.CaddyCapabilities) ([]byte, error) {
	if err := validateCaddyCapabilities(plan, caps); err != nil {
		return nil, err
	}
	httpApp, err := renderHTTPApp(plan, caps)
	if err != nil {
		return nil, err
	}
	tlsApp, err := renderTLSApp(plan)
	if err != nil {
		return nil, err
	}
	cfg := caddyConfig{Admin: map[string]any{"listen": runtimeports.CaddyAdminAddress()}, Apps: map[string]any{}}
	cfg.Apps["http"] = httpApp
	cfg.Apps["tls"] = tlsApp
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
	fallbackBase := plan.FallbackBase
	if fallbackBase == "" {
		fallbackBase = naiveFallbackBase()
	}
	servers := make(map[string]any)
	for key, owner := range plan.Servers {
		serverName := serverNameFor(key)
		server, err := renderServer(key, owner, caps, fallbackBase)
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

func renderTLSApp(plan caddyassembly.CaddyRenderPlan) (map[string]any, error) {
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
		issuer, err := renderACMEIssuer(g.email, g.mode)
		if err != nil {
			return nil, err
		}
		policies = append(policies, map[string]any{
			"subjects": g.domains,
			"issuers":  []map[string]any{issuer},
		})
	}
	tlsApp := map[string]any{"automation": map[string]any{"policies": policies}}
	// Automation policy subjects only select a policy; they do not enroll a
	// name for certificate management. Panel and Naive domains are enrolled by
	// HTTP route host matchers, but a Hysteria2-only domain has no host
	// matcher anywhere, so enroll it explicitly through the automate loader or
	// Caddy never requests its first certificate (audit #308).
	var automate []string
	for _, spec := range specs {
		if !spec.Owners.Panel && len(spec.Owners.NaiveInboundNames) == 0 {
			automate = append(automate, spec.Domain)
		}
	}
	if len(automate) > 0 {
		tlsApp["certificates"] = map[string]any{"automate": automate}
	}
	return tlsApp, nil
}

func renderACMEIssuer(email, mode string) (map[string]any, error) {
	switch mode {
	case "http-01", "tls-alpn-01":
	default:
		return nil, fmt.Errorf("acmeChallengeMode %q is not supported; use http-01 or tls-alpn-01", mode)
	}
	issuer := map[string]any{
		"module": "acme",
		"email":  email,
		"challenges": map[string]any{
			"http":     map[string]any{"disabled": mode != "http-01"},
			"tls-alpn": map[string]any{"disabled": mode != "tls-alpn-01"},
		},
	}
	// Controlled CA plumbing (install-acceptance pebble leg, staging/private
	// CAs): VEIL_ACME_CA_URL points the issuer at a non-Let's-Encrypt
	// directory and VEIL_ACME_CA_ROOT makes Caddy trust that endpoint's
	// self-signed TLS certificate. The values are persisted into veil.env at
	// install so runtime re-renders keep the same CA (audit #304).
	if caURL := acmeIssuerCAURL(); caURL != "" {
		issuer["ca"] = caURL
	}
	if caRoot := acmeIssuerCARoot(); caRoot != "" {
		issuer["trusted_roots_pem_files"] = []string{caRoot}
	}
	return issuer, nil
}

// acmeIssuerCAURL/acmeIssuerCARoot are seams so tests can pin the env contract.
var acmeIssuerCAURL = func() string { return strings.TrimSpace(os.Getenv("VEIL_ACME_CA_URL")) }
var acmeIssuerCARoot = func() string { return strings.TrimSpace(os.Getenv("VEIL_ACME_CA_ROOT")) }

func renderServer(key bindregistry.BindKey, owner caddyassembly.CaddyBindOwner, caps caddycapabilities.CaddyCapabilities, fallbackBase string) (map[string]any, error) {
	server := map[string]any{
		"listen":                  []string{listenString(key)},
		"automatic_https":         map[string]any{"disable_redirects": true},
		"tls_connection_policies": []map[string]any{{}},
	}
	// A Veil-owned :80 listener must also front acme.sh's http-01 challenges:
	// the panel IP certificate renews via --standalone on the internal port
	// this passthrough route proxies to while Caddy holds public :80 (#1181).
	caddyOwnsPublicHTTP := key.Port == 80 && key.Network == bindregistry.ListenTCP
	passthrough := acmeHTTP01PassthroughRoute()
	// Keep Panel HTTPS on TCP only. Caddy's default protocol set includes h3,
	// which binds UDP on the same port and makes Hysteria2 (QUIC/UDP 443)
	// fail live validation and fail to start next to Panel Caddy.
	if owner.Kind == caddyassembly.CaddyOwnerPanel {
		server["protocols"] = []string{"h1", "h2"}
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
		routes := panelRoutes(owner.Domain, owner.BackendPort, owner.WebBasePath, true)
		if caddyOwnsPublicHTTP && passthrough != nil {
			routes = append([]map[string]any{passthrough}, routes...)
		}
		server["routes"] = routes
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
		fallbackRoot, err := resolveNaiveFallbackRoot(owner.FallbackRoot, fallbackBase)
		if err != nil {
			return nil, err
		}
		forwardProxy := map[string]any{
			"handler":          "forward_proxy",
			"hosts":            []string{strings.TrimSpace(owner.Domain)},
			"auth_credentials": authCreds,
			"hide_ip":          true,
			"hide_via":         true,
			"probe_resistance": map[string]any{},
			// The pinned forwardproxy evaluates ACL subjects against the
			// RESOLVED destination when no upstream is configured, so these
			// denies cover DNS names that resolve into the deny set as well as
			// literal IPs. They precede the module's built-in private-range
			// denies (which only cover 10/8, 172.16/12, 192.168/16, ::1 and
			// fe80/10) and extend them to unspecified, CGNAT, link-local,
			// multicast, mapped/NAT64 and ULA destinations (issues #1096,
			// #1097). When a socks5 upstream is configured the module skips
			// its ACL dial check entirely — that bypass is closed by the
			// sing-box leading block rule and the unit IP filter, which sit on
			// the packet path the upstream hop still has to take.
			"acl": []map[string]any{
				{"subjects": EgressDenyCIDRs(), "allow": false},
			},
		}
		if owner.Upstream != "" {
			forwardProxy["upstream"] = owner.Upstream
		}
		handlers := []map[string]any{
			forwardProxy,
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
		// HTTP CONNECT sets Host to the destination (example.com), not the
		// inbound domain, so forward_proxy must stay unmatched. Caddy
		// automatic HTTPS only collects names from HTTP route host matchers
		// (TLS automation subjects are a filter, not an issuance command).
		// Naive-only binds and shared binds whose Naive domain differs from
		// PanelDomain therefore need a host-matched file_server so ACME
		// issues for the inbound name (audit #122 / #209). Same-domain share
		// is already covered by the Panel host matcher.
		domain := strings.TrimSpace(owner.Domain)
		panelDomain := strings.TrimSpace(owner.PanelDomain)
		if domain != "" && !strings.EqualFold(domain, panelDomain) {
			routes = append(routes, map[string]any{
				"match": []map[string]any{{"host": []string{domain}}},
				"handle": []map[string]any{{
					"handler": "file_server",
					"root":    fallbackRoot,
				}},
			})
		}
		routes = append(routes, proxyRoute)
		if caddyOwnsPublicHTTP && passthrough != nil {
			routes = append([]map[string]any{passthrough}, routes...)
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

// resolveNaiveFallbackRoot validates and normalizes the naive fallback root
// against base (the managed <etc>/www tree resolved for this install). The
// legacy /var/lib/veil subtree is rejected outright: veil-caddy.service masks
// /var/lib/veil via InaccessiblePaths, so serving from there is dead
// configuration (issue #618). Relative paths resolve under base; an explicit
// ".." segment is a traversal attempt and fails closed instead of clamping
// back inside the root.
func resolveNaiveFallbackRoot(input, base string) (string, error) {
	base = filepath.ToSlash(filepath.Clean(base))
	if input == "" {
		return base, nil
	}
	root := filepath.ToSlash(filepath.Clean(input))
	if !strings.HasPrefix(root, "/") {
		for _, seg := range strings.Split(root, "/") {
			if seg == ".." {
				return "", fmt.Errorf("fallback root must not contain '..' path traversal: %s", input)
			}
		}
		root = filepath.ToSlash(filepath.Clean(base + "/" + root))
	}
	if !naiveFallbackRootAllowedUnder(root, base) {
		return "", fmt.Errorf("fallback root must be within %s: %s", base, root)
	}
	return root, nil
}

func renderAcmeChallengeServer(key bindregistry.BindKey, owner caddyassembly.AcmeChallengeOwner) map[string]any {
	server := map[string]any{
		"listen":          []string{listenString(key)},
		"automatic_https": map[string]any{"disable_redirects": true},
		"routes":          []map[string]any{},
	}
	// TLS-ALPN-01 is TCP-only. Caddy's default protocol set includes h3,
	// which would bind UDP on the same port and steal it from Hysteria2.
	if key.Port == 443 && key.Network == bindregistry.ListenTCP {
		server["protocols"] = []string{"h1", "h2"}
	}
	if key.Port == 80 && key.Network == bindregistry.ListenTCP {
		// certmagic answers tokens for challenges Caddy itself is solving
		// before routes run; every other token falls through to this
		// passthrough, which is how the panel IP certificate renews through
		// acme.sh --standalone while Caddy owns :80 (#1181). nil (no recorded
		// panel IP identity) leaves the challenge server with no routes —
		// it must not proxy arbitrary hosts to the parked port (#1207).
		if route := acmeHTTP01PassthroughRoute(); route != nil {
			server["routes"] = []map[string]any{route}
		}
	}
	return server
}

// panelIPCertChallengeHosts lists the Host identities the acme.sh challenge
// passthrough may answer — the panel IP certificate's SAN identities, kept
// from the persisted VEIL_PANEL_PUBLIC_IP (#1186). IPv6 literals are emitted
// bracketed and bare because clients differ on which form they send in the
// Host header. A seam keeps tests off the process environment.
var panelIPCertChallengeHosts = func() []string {
	// The process environment wins; a render running outside veil.service
	// (install/repair shells, the privileged helper) falls back to the
	// persisted veil.env so a missing export cannot silently drop the route
	// (#1207).
	raw := os.Getenv("VEIL_PANEL_PUBLIC_IP")
	if raw == "" {
		raw = hostenv.ReadEnvFile(filepath.Join(hostenv.EtcDir(), "veil.env"))["VEIL_PANEL_PUBLIC_IP"]
	}
	var hosts []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		hosts = append(hosts, item)
		if strings.Contains(item, ":") && !strings.HasPrefix(item, "[") {
			hosts = append(hosts, "["+item+"]")
		}
	}
	return hosts
}

// acmeHTTP01PassthroughRoute proxies the ACME http-01 challenge path to the
// internal loopback port acme.sh --standalone binds when the managed Caddy
// edge owns public :80 (IssueOptions.HTTP01ViaCaddy, #1181). It is prepended
// to every Veil-owned :80 server's routes so a panel or naive listener can
// front panel-IP issuance the same way the dedicated -acme challenge server
// does. The handler is deliberately a plain reverse_proxy: no auth, no
// path rewriting — acme.sh serves exactly this prefix itself.
//
// Two narrowings keep the permanent route from being a free proxy to an
// unprivileged port for every terminated host (#1207): the host matcher pins
// it to the panel certificate's own identities (any other Host is left to
// the rest of the routes), and the upstream dials the dedicated 127.42.x
// loopback acme.sh binds — a band no unit-level egress filter pierces, so a
// sandboxed proxy service cannot squat the address between issuances.
// Returns nil when no panel IP identity is recorded: without it the route
// cannot be host-pinned and must not exist.
func acmeHTTP01PassthroughRoute() map[string]any {
	hosts := panelIPCertChallengeHosts()
	if len(hosts) == 0 {
		return nil
	}
	return map[string]any{
		"match": []map[string]any{{
			"host": hosts,
			"path": []string{"/.well-known/acme-challenge/*"},
		}},
		"handle": []map[string]any{{
			"handler": "reverse_proxy",
			"upstreams": []map[string]any{
				{"dial": acmeip.CaddyFrontedHTTP01Host + ":" + portString(acmeip.CaddyFrontedHTTP01Port)},
			},
		}},
		"terminal": true,
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

func panelSubscriptionRoute(domain string, backendPort int) map[string]any {
	match := map[string]any{"path": []string{"/s/*"}}
	if domain != "" {
		match["host"] = []string{domain}
	}
	return map[string]any{
		"match": []map[string]any{match},
		"handle": []map[string]any{{
			"handler":   "reverse_proxy",
			"upstreams": []map[string]any{{"dial": "127.0.0.1:" + portString(backendPort)}},
		}},
		"terminal": true,
	}
}

func panelRoutes(domain string, backendPort int, webBasePath string, includeFallback bool) []map[string]any {
	routes := panelBaseRoutes(domain, backendPort, webBasePath, includeFallback)
	return append([]map[string]any{panelSubscriptionRoute(domain, backendPort)}, routes...)
}

func panelBaseRoutes(domain string, backendPort int, webBasePath string, includeFallback bool) []map[string]any {
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
	// A demoted hy2-only domain keeps http-01 issuance armed even though its
	// challenge server bind was removed — otherwise the default mode would
	// flip it to tls-alpn-01, which a UDP-only domain can never answer, and
	// the deferred-issuance retry window could never converge (#1168).
	if plan.HTTP01DeferredDomains[domain] {
		return "http-01"
	}
	if plan.DefaultChallengeMode != "" {
		return plan.DefaultChallengeMode
	}
	return "tls-alpn-01"
}
