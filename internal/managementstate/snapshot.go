package managementstate

import "github.com/mikkelchokolate/Veil/internal/model"

type SnapshotInput struct {
	Settings      model.Settings
	Inbounds      []model.Inbound
	Rules         []model.RoutingRule
	RoutingPreset string
	RoutingSource model.RoutingSource
	Warp          model.WarpConfig
}

type SnapshotTarget struct {
	Settings      *model.Settings
	Inbounds      *[]model.Inbound
	Rules         *[]model.RoutingRule
	RoutingPreset *string
	RoutingSource *model.RoutingSource
	Warp          *model.WarpConfig
}

func BuildSnapshot(input SnapshotInput) model.ManagementSnapshot {
	return model.ManagementSnapshot{
		Settings:      input.Settings,
		Inbounds:      cloneInbounds(input.Inbounds),
		Rules:         append([]model.RoutingRule(nil), input.Rules...),
		RoutingPreset: input.RoutingPreset,
		RoutingSource: input.RoutingSource,
		Warp:          input.Warp,
	}
}

func ApplySnapshot(target SnapshotTarget, snapshot model.ManagementSnapshot) {
	if target.Settings != nil && snapshot.Settings.PanelListen != "" {
		*target.Settings = MergeSettingsDefaults(snapshot.Settings, *target.Settings)
	}
	if target.Inbounds != nil && snapshot.Inbounds != nil {
		*target.Inbounds = cloneInbounds(snapshot.Inbounds)
	}
	if target.Rules != nil && snapshot.Rules != nil {
		*target.Rules = append([]model.RoutingRule(nil), snapshot.Rules...)
	}
	if target.RoutingPreset != nil && snapshot.RoutingPreset != "" {
		*target.RoutingPreset = snapshot.RoutingPreset
	}
	if target.RoutingSource != nil && (snapshot.RoutingSource.Repository != "" || len(snapshot.RoutingSource.Files) > 0) {
		*target.RoutingSource = snapshot.RoutingSource
	}
	if target.Warp != nil && (snapshot.Warp.Endpoint != "" || snapshot.Warp.Enabled || snapshot.Warp.LicenseKey != "") {
		*target.Warp = snapshot.Warp
	}
}

func MergeSettingsDefaults(settings model.Settings, defaults model.Settings) model.Settings {
	merged := settings
	if merged.PanelListen == "" {
		merged.PanelListen = defaults.PanelListen
	}
	if merged.PanelAccess == "" {
		merged.PanelAccess = defaults.PanelAccess
	}
	if merged.WebBasePath == "" {
		merged.WebBasePath = defaults.WebBasePath
	}
	if merged.Domain == "" {
		merged.Domain = defaults.Domain
	}
	if merged.Email == "" {
		merged.Email = defaults.Email
	}
	return merged
}

func cloneInbounds(inbounds []model.Inbound) []model.Inbound {
	if inbounds == nil {
		return nil
	}
	out := make([]model.Inbound, len(inbounds))
	for i, inbound := range inbounds {
		out[i] = inbound
		out[i].Profiles = append([]model.ClientProfile(nil), inbound.Profiles...)
	}
	return out
}
