package naiveproxy

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

// RenderConfig generates the consolidated Caddy JSON config for all naiveproxy
// inbounds (and panel access when panelAccess is caddy). The inbound/Caddy
// redesign moves from per-inbound Caddyfiles to a single JSON config used by
// veil-caddy.service.
func (Plugin) RenderConfig(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
	if len(input.Inbounds) == 0 {
		return nil, false, nil
	}

	plan, _, _, err := caddyassembly.BuildFinalRenderPlan(input.Settings, input.Inbounds)
	if err != nil {
		return nil, false, err
	}
	applyNaiveLiveUsers(plan, input.Settings, input.Inbounds)
	if input.Warp.Enabled {
		socksPort := input.Warp.SocksPort
		if socksPort == 0 {
			socksPort = 40000
		}
		for key, owner := range plan.Servers {
			if owner.Kind == caddyassembly.CaddyOwnerNaive {
				owner.Upstream = "socks5://127.0.0.1:" + strconv.Itoa(socksPort)
				plan.Servers[key] = owner
			}
		}
	}

	caps, err := caddycapabilities.Probe("")
	if err != nil {
		if !caddycapabilities.IsMissingBinary(err) {
			return nil, false, fmt.Errorf("failed to probe Caddy capabilities: %w", err)
		}
		// A Hysteria2-only plan (audit #156) renders this artifact before the
		// Caddy runtime exists; tolerate a missing binary the way the
		// panelaccess path does. Naive inbounds still fail hard: they need the
		// forward_proxy module probed.
		for _, inb := range input.Inbounds {
			if inb.Protocol == "naiveproxy" && inb.Enabled {
				return nil, false, fmt.Errorf("failed to probe Caddy capabilities: %w", err)
			}
		}
		caps = caddycapabilities.CaddyCapabilities{}
	}
	data, err := renderer.RenderCaddyJSON(plan, caps)
	if err != nil {
		return nil, false, err
	}

	return []generatedconfig.GeneratedConfigArtifact{{
		Path: input.Paths.CaddyJSON(),
		Body: string(data),
	}}, true, nil
}

// ArtifactSpec returns the artifact metadata for the consolidated Caddy JSON
// config.
func (Plugin) ArtifactSpec() generatedconfig.ArtifactSpec {
	return generatedconfig.ArtifactSpec{
		Subpath:        generatedconfig.CaddyJSONConfigSubpath,
		ValidationName: "caddy",
		ValidationCommand: func(path string) []string {
			return []string{"caddy", "validate", "--config", path}
		},
	}
}

func applyNaiveLiveUsers(plan caddyassembly.CaddyRenderPlan, settings model.Settings, inbounds []model.Inbound) {
	byName := map[string]model.Inbound{}
	for _, inbound := range inbounds {
		if inbound.Protocol == "naiveproxy" {
			byName[inbound.Name] = inbound
		}
	}
	for key, owner := range plan.Servers {
		if owner.Kind != caddyassembly.CaddyOwnerNaive {
			continue
		}
		inbound, ok := byName[owner.InboundName]
		if !ok {
			continue
		}
		owner.NaiveUsers = liveNaiveUsers(settings, inbound)
		plan.Servers[key] = owner
	}
}

func liveNaiveUsers(settings model.Settings, inbound model.Inbound) []caddyassembly.CaddyNaiveUser {
	var users []caddyassembly.CaddyNaiveUser
	runtimeUsers := make(map[string]caddyassembly.CaddyNaiveUser, len(inbound.RuntimeCredentials))
	for _, credential := range inbound.RuntimeCredentials {
		username := strings.TrimSpace(credential.Username)
		password := strings.TrimSpace(credential.Password)
		if username != "" && password != "" {
			runtimeUsers[username] = caddyassembly.CaddyNaiveUser{Username: username, Password: password}
		}
	}
	for _, profile := range inbound.Profiles {
		if !profile.Enabled || strings.TrimSpace(profile.Username) == "" || strings.TrimSpace(profile.Password) == "" {
			continue
		}
		if _, replaced := runtimeUsers[profile.Username]; !replaced {
			users = append(users, caddyassembly.CaddyNaiveUser{Username: profile.Username, Password: profile.Password})
		}
	}
	for _, credential := range inbound.RuntimeCredentials {
		if user, ok := runtimeUsers[strings.TrimSpace(credential.Username)]; ok {
			users = append(users, user)
			delete(runtimeUsers, user.Username)
		}
	}
	if len(users) > 0 || len(inbound.Profiles) > 0 {
		return users
	}
	username := naiveUsername(settings, inbound)
	password := naivePassword(settings, inbound)
	if username != "" && password != "" {
		return []caddyassembly.CaddyNaiveUser{{Username: username, Password: password}}
	}
	return nil
}
