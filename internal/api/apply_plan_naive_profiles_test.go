package api

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildApplyPlanAcceptsNaiveProxyWithProfileCredentials(t *testing.T) {
	plan := BuildApplyPlan(ApplyPlanInput{
		Settings: Settings{
			PanelListen: "127.0.0.1:2096",
			Mode:        "dev",
			Domain:      "vpn.example.com",
			Email:       "admin@example.com",
		},
		Inbounds: []Inbound{{
			Name:      "naive",
			Protocol:  "naiveproxy",
			Transport: "tcp",
			Port:      443,
			Enabled:   true,
			Profiles: []model.ClientProfile{{
				Name:     "alice",
				Password: "secret",
				Enabled:  true,
			}},
		}},
	})

	if !plan.Valid {
		t.Fatalf("NaiveProxy profile-only plan should be valid: %+v", plan)
	}
}
