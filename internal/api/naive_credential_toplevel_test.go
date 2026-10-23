package api

import (
	"context"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

// naiveHasCredential must accept the top-level inbound.Password that
// clientaccess.InboundCredentialPolicy.ApplyCreate always generates for a
// fresh naiveproxy inbound. The renderer (caddyassembly.NaiveUsers) already
// honours inbound.Password, but the validator did not, so a create-then-apply
// flow was incorrectly rejected.
func TestBuildApplyPlanAcceptsNaiveProxyWithTopLevelPassword(t *testing.T) {
	stubCaddyProbe(t, func(context.Context, string) (caddycapabilities.CaddyCapabilities, error) {
		return caddycapabilities.CaddyCapabilities{ForwardProxy: true, HTTP3: true}, nil
	})
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "vpn.example.com", DefaultAcmeEmail: "admin@example.com"},
		Inbounds: []Inbound{{
			Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true,
			Password:       "generated-secret",
			ProtocolFields: map[string]any{"domain": "vpn.example.com", "email": "admin@example.com"},
		}},
	})
	if !plan.Valid {
		t.Fatalf("NaiveProxy with top-level password should be valid: %+v", plan.Errors)
	}
	if strings.Contains(strings.Join(plan.Errors, "\n"), "missing valid credentials") {
		t.Fatalf("unexpected missing-credential error: %+v", plan.Errors)
	}
}
