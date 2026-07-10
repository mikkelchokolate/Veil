package generatedconfig

// GeneratedRenderSettingsPolicy decides whether enough global and per-inbound
// configuration is present to attempt rendering generated config artifacts.
//
// After the protocol-field migration, protocol-specific values live in the
// dynamic ProtocolFields maps as well as the legacy flat fields. The policy
// checks both locations so protocols that require render settings (currently
// Hysteria2) are not silently skipped when only ProtocolFields are populated.
type GeneratedRenderSettingsPolicy struct{}

func NewGeneratedRenderSettingsPolicy() GeneratedRenderSettingsPolicy {
	return GeneratedRenderSettingsPolicy{}
}

func (GeneratedRenderSettingsPolicy) HasRenderSettings(settings Settings, inbounds []Inbound) bool {
	if settings.Domain != "" || settings.Email != "" {
		return true
	}
	if settingsHaveRenderSettings(settings) {
		return true
	}
	for _, inbound := range inbounds {
		if !inbound.Enabled {
			continue
		}
		if inbound.Password != "" || len(inbound.Profiles) > 0 {
			return true
		}
		if inboundHaveRenderSettings(inbound) {
			return true
		}
	}
	return false
}

// renderSettingsProtocolFieldKeys are the protocol-specific settings keys that
// make generated-config rendering worth attempting. They correspond to the
// legacy flat fields checked by the original policy and the keys used in the
// dynamic ProtocolFields maps for NaiveProxy and Hysteria2.
var renderSettingsProtocolFieldKeys = []string{
	"naiveUsername",
	"naivePassword",
	"hysteria2Password",
	"masqueradeURL",
	"fallbackRoot",
}

func settingsHaveRenderSettings(settings Settings) bool {
	if settings.ProtocolFields != nil {
		for _, key := range renderSettingsProtocolFieldKeys {
			if s, ok := settings.ProtocolFields[key].(string); ok && s != "" {
				return true
			}
		}
	}
	return settings.NaiveUsername != "" ||
		settings.NaivePassword != "" ||
		settings.Hysteria2Password != "" ||
		settings.MasqueradeURL != "" ||
		settings.FallbackRoot != ""
}

func inboundHaveRenderSettings(inbound Inbound) bool {
	if inbound.ProtocolFields != nil {
		for _, key := range renderSettingsProtocolFieldKeys {
			if s, ok := inbound.ProtocolFields[key].(string); ok && s != "" {
				return true
			}
		}
	}
	return inbound.NaiveUsername != "" ||
		inbound.NaivePassword != "" ||
		inbound.Hysteria2Password != "" ||
		inbound.MasqueradeURL != "" ||
		inbound.FallbackRoot != ""
}
