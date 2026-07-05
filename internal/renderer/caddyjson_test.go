package renderer

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

func TestRenderCaddyJSONNaiveForwardProxyOrder(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}: {
				Kind:        caddyassembly.CaddyOwnerNaive,
				Domain:      "p.example.com",
				InboundName: "naive-1",
				Transport:   "tcp",
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"p.example.com": {Domain: "p.example.com", Email: "a@example.com"},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !containsInOrder(s, `"forward_proxy"`, `"file_server"`) {
		t.Error("forward_proxy must appear before file_server")
	}
}

func containsInOrder(s, a, b string) bool {
	ia := strings.Index(s, a)
	ib := strings.Index(s, b)
	return ia >= 0 && ib > ia
}

func TestRenderCaddyJSONPanelOnly(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}: {
				Kind:        caddyassembly.CaddyOwnerPanel,
				Domain:      "panel.example.com",
				BackendPort: 2096,
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"panel.example.com": {Domain: "panel.example.com", Email: "admin@example.com"},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	apps := cfg["apps"].(map[string]any)
	if _, ok := apps["http"]; !ok {
		t.Error("expected http app")
	}
}

func TestRenderCaddyJSONAcmeChallengeKeys(t *testing.T) {
	tests := []struct {
		name    string
		key     bindregistry.BindKey
		mode    string
		wantKey string
	}{
		{
			name:    "http-01",
			key:     bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP},
			mode:    "http-01",
			wantKey: `"http"`,
		},
		{
			name:    "tls-alpn-01",
			key:     bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP},
			mode:    "tls-alpn-01",
			wantKey: `"tls-alpn"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := caddyassembly.CaddyRenderPlan{
				Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
					{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}: {
						Kind:        caddyassembly.CaddyOwnerPanel,
						Domain:      "panel.example.com",
						BackendPort: 2096,
					},
				},
				Domains: map[string]caddyassembly.CaddyDomainCertSpec{
					"panel.example.com": {Domain: "panel.example.com", Email: "admin@example.com"},
				},
				ACMEChallenges: map[bindregistry.BindKey]caddyassembly.AcmeChallengeOwner{
					tt.key: {ChallengeMode: tt.mode, Domains: []string{"panel.example.com"}},
				},
			}
			data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
			if err != nil {
				t.Fatal(err)
			}
			s := string(data)
			if !strings.Contains(s, `"issuers"`) {
				t.Error("expected automation policy to contain issuers array")
			}
			if !strings.Contains(s, tt.wantKey) {
				t.Errorf("expected challenge key %s in rendered JSON", tt.wantKey)
			}
			if strings.Contains(s, `"http-01"`) || strings.Contains(s, `"tls-alpn-01"`) {
				t.Error("old challenge keys http-01/tls-alpn-01 must not appear in rendered JSON")
			}
		})
	}
}

func TestRenderCaddyJSONRejectsNaiveWithoutForwardProxy(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}: {
				Kind:        caddyassembly.CaddyOwnerNaive,
				Domain:      "p.example.com",
				InboundName: "naive-1",
				Transport:   "tcp",
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"p.example.com": {Domain: "p.example.com", Email: "a@example.com"},
		},
	}
	_, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: false})
	if err == nil {
		t.Fatal("expected error for missing forward_proxy module")
	}
	want := "Caddy binary does not include the forward_proxy module required for NaiveProxy"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestRenderCaddyJSONAllowsNaiveTCPWithForwardProxy(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}: {
				Kind:        caddyassembly.CaddyOwnerNaive,
				Domain:      "p.example.com",
				InboundName: "naive-1",
				Transport:   "tcp",
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"p.example.com": {Domain: "p.example.com", Email: "a@example.com"},
		},
	}
	_, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
	if err != nil {
		t.Fatalf("expected success for tcp transport with forward_proxy, got %v", err)
	}
}

func TestRenderCaddyJSONNaiveFallbackRootDefaults(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}: {
				Kind:        caddyassembly.CaddyOwnerNaive,
				Domain:      "p.example.com",
				InboundName: "naive-1",
				Transport:   "tcp",
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"p.example.com": {Domain: "p.example.com", Email: "a@example.com"},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"root": "/var/lib/veil/www"`) {
		t.Errorf("expected default fallback root, got %s", string(data))
	}
}

func TestRenderCaddyJSONNaiveFallbackRootRejectsParentTraversal(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}: {
				Kind:         caddyassembly.CaddyOwnerNaive,
				Domain:       "p.example.com",
				InboundName:  "naive-1",
				Transport:    "tcp",
				FallbackRoot: "..",
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"p.example.com": {Domain: "p.example.com", Email: "a@example.com"},
		},
	}
	_, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
	if err == nil {
		t.Fatal("expected error for fallback root outside /var/lib/veil")
	}
}

func TestRenderCaddyJSONNaiveFallbackRootRejectsOutsideVeil(t *testing.T) {
	cases := []string{
		"/etc/passwd",
		"/var/lib/veil2",
		"/var/lib/veil-evil",
		"/var/lib/veil2/sub",
		"/var/lib",
	}
	for _, root := range cases {
		t.Run(root, func(t *testing.T) {
			plan := caddyassembly.CaddyRenderPlan{
				Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
					{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}: {
						Kind:         caddyassembly.CaddyOwnerNaive,
						Domain:       "p.example.com",
						InboundName:  "naive-1",
						Transport:    "tcp",
						FallbackRoot: root,
					},
				},
				Domains: map[string]caddyassembly.CaddyDomainCertSpec{
					"p.example.com": {Domain: "p.example.com", Email: "a@example.com"},
				},
			}
			_, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
			if err == nil {
				t.Fatalf("expected error for fallback root %q outside /var/lib/veil", root)
			}
		})
	}
}

func TestRenderCaddyJSONNaiveFallbackRootAcceptsWithinVeil(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/var/lib/veil", "/var/lib/veil"},
		{"/var/lib/veil/", "/var/lib/veil"},
		{"/var/lib/veil/custom", "/var/lib/veil/custom"},
		{"/var/lib/veil/www", "/var/lib/veil/www"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			plan := caddyassembly.CaddyRenderPlan{
				Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
					{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}: {
						Kind:         caddyassembly.CaddyOwnerNaive,
						Domain:       "p.example.com",
						InboundName:  "naive-1",
						Transport:    "tcp",
						FallbackRoot: tc.input,
					},
				},
				Domains: map[string]caddyassembly.CaddyDomainCertSpec{
					"p.example.com": {Domain: "p.example.com", Email: "a@example.com"},
				},
			}
			data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
			if err != nil {
				t.Fatal(err)
			}
			wantRoot := fmt.Sprintf(`"root": %q`, tc.want)
			if !strings.Contains(string(data), wantRoot) {
				t.Errorf("expected %s in rendered JSON, got %s", wantRoot, string(data))
			}
		})
	}
}

func TestRenderCaddyJSONNaiveFallbackRootResolvesRelativePath(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}: {
				Kind:         caddyassembly.CaddyOwnerNaive,
				Domain:       "p.example.com",
				InboundName:  "naive-1",
				Transport:    "tcp",
				FallbackRoot: "custom",
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"p.example.com": {Domain: "p.example.com", Email: "a@example.com"},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"root": "/var/lib/veil/custom"`) {
		t.Errorf("expected resolved fallback root, got %s", string(data))
	}
}

func TestRenderCaddyJSONUsesAutomaticHTTPS(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}: {
				Kind:        caddyassembly.CaddyOwnerPanel,
				Domain:      "panel.example.com",
				BackendPort: 2096,
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"panel.example.com": {Domain: "panel.example.com", Email: "admin@example.com"},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, `"auto_https"`) {
		t.Error("rendered JSON must not contain the Caddyfile field auto_https")
	}
	if !strings.Contains(s, `"automatic_https"`) {
		t.Error("rendered JSON must contain the JSON field automatic_https")
	}
}

func TestRenderCaddyJSONNaiveForwardProxyAuthCredentials(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}: {
				Kind:        caddyassembly.CaddyOwnerNaive,
				Domain:      "p.example.com",
				InboundName: "naive-1",
				Transport:   "tcp",
				NaiveUsers: []caddyassembly.CaddyNaiveUser{
					{Username: "alice", Password: "secret-a"},
					{Username: "bob", Password: "secret-b"},
				},
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"p.example.com": {Domain: "p.example.com", Email: "a@example.com"},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, `"basic_auth"`) {
		t.Error("rendered JSON must not contain the Caddyfile field basic_auth")
	}
	if !strings.Contains(s, `"auth_credentials"`) {
		t.Error("rendered JSON must contain the JSON field auth_credentials")
	}
	for _, u := range []struct{ user, pass string }{
		{"alice", "secret-a"},
		{"bob", "secret-b"},
	} {
		want := base64.StdEncoding.EncodeToString([]byte(u.user + ":" + u.pass))
		if !strings.Contains(s, fmt.Sprintf("%q", want)) {
			t.Errorf("expected base64 credential %q for %s", want, u.user)
		}
	}
}

func TestRenderCaddyJSONAdminEndpoint(t *testing.T) {
	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}: {
				Kind:        caddyassembly.CaddyOwnerPanel,
				Domain:      "panel.example.com",
				BackendPort: 2096,
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"panel.example.com": {Domain: "panel.example.com", Email: "admin@example.com"},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	admin, ok := cfg["admin"].(map[string]any)
	if !ok {
		t.Fatalf("expected top-level admin block, got %T", cfg["admin"])
	}
	if admin["listen"] != "127.0.0.1:2019" {
		t.Errorf("admin.listen = %v, want 127.0.0.1:2019", admin["listen"])
	}
}

func TestRenderCaddyJSONValidatesWithCaddy(t *testing.T) {
	if _, err := exec.LookPath("caddy"); err != nil {
		t.Skip("caddy binary not available in PATH:", err)
	}

	plan := caddyassembly.CaddyRenderPlan{
		Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
			{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}: {
				Kind:        caddyassembly.CaddyOwnerPanel,
				Domain:      "panel.example.com",
				BackendPort: 2096,
			},
		},
		Domains: map[string]caddyassembly.CaddyDomainCertSpec{
			"panel.example.com": {Domain: "panel.example.com", Email: "admin@example.com"},
		},
	}
	data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
	if err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command("caddy", "validate", "--config", cfgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("caddy validate failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Valid configuration") {
		t.Errorf("caddy validate did not report valid configuration:\n%s", out)
	}
}
