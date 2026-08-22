package managementstate

import "github.com/mikkelchokolate/Veil/internal/model"

type SnapshotInput struct {
	EffectiveAt   int64
	Setup         model.SetupState
	Settings      model.Settings
	Inbounds      []model.Inbound
	Rules         []model.RoutingRule
	RoutingPreset string
	RoutingSource model.RoutingSource
	Warp          model.WarpConfig
	Users         []model.User
	// A3: normalized client state that affects runtime rendering.
	Clients     []model.ClientSnapshot
	Bindings    []model.BindingSnapshot
	Credentials []model.CredentialSnapshot
}

type SnapshotTarget struct {
	Setup         *model.SetupState
	Settings      *model.Settings
	Inbounds      *[]model.Inbound
	Rules         *[]model.RoutingRule
	RoutingPreset *string
	RoutingSource *model.RoutingSource
	Warp          *model.WarpConfig
	Users         *[]model.User
}

func BuildSnapshot(input SnapshotInput) model.ManagementSnapshot {
	return model.ManagementSnapshot{
		EffectiveAt:   input.EffectiveAt,
		Setup:         input.Setup,
		Settings:      cloneSettings(input.Settings),
		Inbounds:      cloneInbounds(input.Inbounds),
		Rules:         append([]model.RoutingRule(nil), input.Rules...),
		RoutingPreset: input.RoutingPreset,
		RoutingSource: cloneRoutingSource(input.RoutingSource),
		Warp:          cloneWarp(input.Warp),
		Users:         cloneUsers(input.Users),
		Clients:       append([]model.ClientSnapshot(nil), input.Clients...),
		Bindings:      append([]model.BindingSnapshot(nil), input.Bindings...),
		Credentials:   append([]model.CredentialSnapshot(nil), input.Credentials...),
	}
}

func cloneSettings(settings model.Settings) model.Settings {
	cloned := settings
	cloned.ProtocolFields = cloneProtocolFields(settings.ProtocolFields)
	return cloned
}

func ApplySnapshot(target SnapshotTarget, snapshot model.ManagementSnapshot) {
	if target.Setup != nil {
		*target.Setup = snapshot.Setup
	}
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
		*target.RoutingSource = cloneRoutingSource(snapshot.RoutingSource)
	}
	if target.Warp != nil && (snapshot.Warp.Endpoint != "" || snapshot.Warp.Enabled || snapshot.Warp.LicenseKey != "") {
		*target.Warp = cloneWarp(snapshot.Warp)
	}
	if target.Users != nil && snapshot.Users != nil {
		*target.Users = cloneUsers(snapshot.Users)
	}
}

func MergeSettingsDefaults(settings model.Settings, defaults model.Settings) model.Settings {
	merged := settings
	merged.ProtocolFields = cloneProtocolFields(settings.ProtocolFields)
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
		out[i].ProtocolFields = cloneProtocolFields(inbound.ProtocolFields)
	}
	return out
}

func cloneProtocolFields(pf map[string]any) map[string]any {
	if pf == nil {
		return nil
	}
	out := make(map[string]any, len(pf))
	for k, v := range pf {
		out[k] = v
	}
	return out
}

func cloneUsers(users []model.User) []model.User {
	if users == nil {
		return nil
	}
	out := make([]model.User, len(users))
	copy(out, users)
	return out
}

func cloneRoutingSource(source model.RoutingSource) model.RoutingSource {
	if source.Files == nil {
		return source
	}
	cloned := source
	cloned.Files = append([]model.RoutingSourceFile(nil), source.Files...)
	return cloned
}

func cloneWarp(warp model.WarpConfig) model.WarpConfig {
	if warp.Reserved == nil {
		return warp
	}
	cloned := warp
	cloned.Reserved = append([]int(nil), warp.Reserved...)
	return cloned
}
