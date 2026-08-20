package renderer_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

func TestRenderCaddyJSON_NaiveTCP443HappyPath(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}: {
				Kind:         caddyassembly.CaddyOwnerNaive,
				Domain:       "vpn.example.com",
				InboundName:  "test",
				Transport:    "tcp",
				NaiveUsers:   []caddyassembly.CaddyNaiveUser{{Username: "user", Password: "pass"}},
				FallbackRoot: "/var/lib/veil/www",
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"vpn.example.com": {
				Domain: "vpn.example.com",
				Email:  "admin@vpn.example.com",
			},
		},
		DefaultChallengeMode: "tls-alpn-01",
	}

	data, err := renderer.RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
	if err != nil {
		t.Fatalf("RenderCaddyJSON error: %v", err)
	}
	t.Logf("rendered Caddy JSON:\n%s", string(data))

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("rendered JSON is invalid: %v\n%s", err, string(data))
	}

	apps := cfg["apps"].(map[string]any)
	httpApp := apps["http"].(map[string]any)
	servers := httpApp["servers"].(map[string]any)

	serverName := "tcp-0.0.0.0-443"
	server, ok := servers[serverName].(map[string]any)
	if !ok {
		t.Fatalf("expected server %s in %v", serverName, servers)
	}

	listen := server["listen"].([]any)
	if len(listen) != 1 || listen[0] != ":443" {
		t.Errorf("server.listen = %v, want [:443]", listen)
	}

	routes := server["routes"].([]any)
	// Host matcher stays on a file_server route so Caddy still discovers the
	// inbound domain for certificates (audit #122). forward_proxy must be
	// unmatched: CONNECT uses the destination as Host.
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes (host-matched file_server + unmatched proxy), got %d", len(routes))
	}
	certRoute := routes[0].(map[string]any)
	matches, ok := certRoute["match"].([]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("cert-discovery route must carry a host matcher: %v", certRoute)
	}
	hostMatch := matches[0].(map[string]any)
	hosts, ok := hostMatch["host"].([]any)
	if !ok || len(hosts) != 1 || hosts[0] != "vpn.example.com" {
		t.Fatalf("host matcher = %v, want [vpn.example.com]", hostMatch)
	}
	certHandlers := certRoute["handle"].([]any)
	if len(certHandlers) != 1 || certHandlers[0].(map[string]any)["handler"] != "file_server" {
		t.Fatalf("cert-discovery handlers = %v, want file_server", certHandlers)
	}

	route := routes[1].(map[string]any)
	if _, matched := route["match"]; matched {
		t.Fatalf("forward_proxy route must be unmatched so CONNECT sees destination hosts: %v", route)
	}

	handlers := route["handle"].([]any)
	if len(handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(handlers))
	}
	first := handlers[0].(map[string]any)
	second := handlers[1].(map[string]any)
	if first["handler"] != "forward_proxy" {
		t.Errorf("first handler = %v, want forward_proxy", first["handler"])
	}
	if second["handler"] != "file_server" {
		t.Errorf("second handler = %v, want file_server", second["handler"])
	}
	if second["root"] != "/var/lib/veil/www" {
		t.Errorf("file_server root = %v, want /var/lib/veil/www", second["root"])
	}

	authCreds := first["auth_credentials"].([]any)
	basicValue := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	wantCred := base64.StdEncoding.EncodeToString([]byte(basicValue))
	if len(authCreds) != 1 || authCreds[0] != wantCred {
		t.Errorf("auth_credentials = %v, want [%s]", authCreds, wantCred)
	}

	tlsApp := apps["tls"].(map[string]any)
	automation := tlsApp["automation"].(map[string]any)
	policies := automation["policies"].([]any)
	if len(policies) != 1 {
		t.Fatalf("expected 1 automation policy, got %d", len(policies))
	}
	policy := policies[0].(map[string]any)
	subjects := policy["subjects"].([]any)
	if len(subjects) != 1 || subjects[0] != "vpn.example.com" {
		t.Errorf("policy.subjects = %v, want [vpn.example.com]", subjects)
	}
	issuers := policy["issuers"].([]any)
	if len(issuers) != 1 {
		t.Fatalf("expected 1 issuer, got %d", len(issuers))
	}
	issuer := issuers[0].(map[string]any)
	if issuer["module"] != "acme" {
		t.Errorf("issuer.module = %v, want acme", issuer["module"])
	}
	if issuer["email"] != "admin@vpn.example.com" {
		t.Errorf("issuer.email = %v, want admin@vpn.example.com", issuer["email"])
	}
	challenges := issuer["challenges"].(map[string]any)
	httpChallenge := challenges["http"].(map[string]any)
	tlsAlpnChallenge := challenges["tls-alpn"].(map[string]any)
	if httpChallenge["disabled"] != true {
		t.Error("http challenge must be disabled for tls-alpn-01 mode")
	}
	if tlsAlpnChallenge["disabled"] != false {
		t.Error("tls-alpn challenge must be enabled for tls-alpn-01 mode")
	}
}

// TestRenderCaddyJSON_SameDomainTwoServersSharesCert verifies that two naive
// inbounds using the same domain on different ports render distinct servers
// while sharing a single TLS automation policy.
func TestRenderCaddyJSON_SameDomainTwoServersSharesCert(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}: {
				Kind:        caddyassembly.CaddyOwnerNaive,
				Domain:      "vpn.example.com",
				InboundName: "test-a",
				Transport:   "tcp",
				NaiveUsers:  []caddyassembly.CaddyNaiveUser{{Username: "user-a", Password: "pass-a"}},
			},
			{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}: {
				Kind:        caddyassembly.CaddyOwnerNaive,
				Domain:      "vpn.example.com",
				InboundName: "test-b",
				Transport:   "tcp",
				NaiveUsers:  []caddyassembly.CaddyNaiveUser{{Username: "user-b", Password: "pass-b"}},
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"vpn.example.com": {
				Domain: "vpn.example.com",
				Email:  "admin@vpn.example.com",
			},
		},
		DefaultChallengeMode: "tls-alpn-01",
	}

	data, err := renderer.RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
	if err != nil {
		t.Fatalf("RenderCaddyJSON error: %v", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("rendered JSON is invalid: %v\n%s", err, string(data))
	}

	apps := cfg["apps"].(map[string]any)
	servers := apps["http"].(map[string]any)["servers"].(map[string]any)
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}

	tlsApp := apps["tls"].(map[string]any)
	policies := tlsApp["automation"].(map[string]any)["policies"].([]any)
	if len(policies) != 1 {
		t.Fatalf("expected 1 shared automation policy, got %d", len(policies))
	}
	policy := policies[0].(map[string]any)
	subjects := policy["subjects"].([]any)
	if len(subjects) != 1 || subjects[0] != "vpn.example.com" {
		t.Errorf("policy.subjects = %v, want [vpn.example.com]", subjects)
	}
}
