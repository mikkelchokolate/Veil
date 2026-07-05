package renderer

import (
	"encoding/json"
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
	byIssuer := make(map[string][]string)
	for _, spec := range plan.Domains {
		issuerKey := spec.Email + "/" + challengeForDomain(plan, spec.Domain)
		byIssuer[issuerKey] = append(byIssuer[issuerKey], spec.Domain)
	}
	var policies []map[string]any
	for _, domains := range byIssuer {
		policies = append(policies, map[string]any{
			"subjects": domains,
			"issuer":   map[string]any{"email": domains[0], "module": "acme"},
		})
	}
	return map[string]any{"automation": map[string]any{"policies": policies}}
}

func renderServer(key bindregistry.BindKey, owner caddyassembly.CaddyBindOwner, caps caddycapabilities.CaddyCapabilities) map[string]any {
	// Naive server: no host matcher, forward_proxy first, file_server fallback.
	// Panel server: host matcher, reverse_proxy to panel loopback.
	// Implementation expanded in Task 11.
	return map[string]any{
		"listen": []string{listenString(key)},
		"routes": []map[string]any{},
	}
}

func renderAcmeChallengeServer(key bindregistry.BindKey, owner caddyassembly.AcmeChallengeOwner) map[string]any {
	return map[string]any{
		"listen": []string{listenString(key)},
		"routes": []map[string]any{},
	}
}

func serverNameFor(key bindregistry.BindKey) string {
	return string(key.Network) + "-" + key.Address + "-" + portString(key.Port)
}

func listenString(key bindregistry.BindKey) string {
	return ":" + portString(key.Port)
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
