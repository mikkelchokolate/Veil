package renderer

import (
	"encoding/json"
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
				Kind:   caddyassembly.CaddyOwnerPanel,
				Domain: "panel.example.com",
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
