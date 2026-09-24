package api

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildApplyPlanAcceptsNaiveProxyWithProfileCredentials(t *testing.T) {
	// Same accepted-plan contract as the caddy-settings sisters (#846): stub
	// the probe so validity does not depend on a host binary, then pin the
	// consolidated caddy config leg, reload action, and runtime.
	stubCaddyProbe(t, func(string) (caddycapabilities.CaddyCapabilities, error) {
		return caddycapabilities.CaddyCapabilities{ForwardProxy: true, HTTP3: true}, nil
	})
	plan := BuildApplyPlan(ApplyPlanInput{
		LiveRoot: "/etc/veil/generated",
		Settings: Settings{
			PanelListen:      "127.0.0.1:2096",
			Mode:             "dev",
			Domain:           "vpn.example.com",
			DefaultAcmeEmail: "admin@example.com",
		},
		Inbounds: []Inbound{{
			Name:      "naive",
			Protocol:  "naiveproxy",
			Transport: "tcp",
			Port:      443,
			Enabled:   true,
			Profiles: []model.ClientProfile{{
				Name:     "alice",
				Username: "alice",
				Password: "secret",
				Enabled:  true,
			}},
		}},
	})

	if !plan.Valid {
		t.Fatalf("NaiveProxy profile-only plan should be valid: %+v", plan)
	}
	if !containsString(plan.Configs, "/etc/veil/generated/caddy/config.json") || !containsString(plan.Actions, "reload veil-caddy.service") || !containsString(plan.Runtimes, "veil-caddy.service") {
		t.Fatalf("accepted naive profile plan missing caddy config/action/runtime: %+v", plan)
	}
}
