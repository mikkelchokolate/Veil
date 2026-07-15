package generatedconfig

// GeneratedRenderSettingsPolicy decides whether enough global and per-inbound
// configuration is present to attempt rendering generated config artifacts.
//
// After the protocol-field migration, protocol-specific values live in the
// dynamic ProtocolFields maps as well as the legacy flat fields. The policy
// checks both locations so protocols that require render settings (currently
// Hysteria2) are not silently skipped when only ProtocolFields are populated.
//
// A field-key provider can be supplied so the policy stays in sync with the
// protocol registry instead of hardcoding protocol-specific keys.
type GeneratedRenderSettingsPolicy struct {
	fieldKeys []string
}

func NewGeneratedRenderSettingsPolicy() GeneratedRenderSettingsPolicy {
	return GeneratedRenderSettingsPolicy{}
}

// NewGeneratedRenderSettingsPolicyWithFieldKeys creates a policy that treats
// the supplied protocol-field keys as render-relevant. This lets callers build
// the key set from the current protocol registry rather than the built-in
// legacy list.
func NewGeneratedRenderSettingsPolicyWithFieldKeys(fieldKeys []string) GeneratedRenderSettingsPolicy {
	keys := make([]string, len(fieldKeys))
	copy(keys, fieldKeys)
	return GeneratedRenderSettingsPolicy{fieldKeys: keys}
}

func (p GeneratedRenderSettingsPolicy) HasRenderSettings(settings Settings, inbounds []Inbound) bool {
	if settings.Domain != "" || settings.Email != "" {
		return true
	}
	keys := p.protocolFieldKeys()
	if settingsHaveRenderSettings(settings, keys) {
		return true
	}
	for _, inbound := range inbounds {
		if !inbound.Enabled {
			continue
		}
		if inbound.Password != "" || len(inbound.Profiles) > 0 {
			return true
		}
		if inboundHaveRenderSettings(inbound, keys) {
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

func (p GeneratedRenderSettingsPolicy) protocolFieldKeys() []string {
	if len(p.fieldKeys) > 0 {
		return p.fieldKeys
	}
	return renderSettingsProtocolFieldKeys
}

func settingsHaveRenderSettings(settings Settings, keys []string) bool {
	if settings.ProtocolFields != nil {
		for _, key := range keys {
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

func inboundHaveRenderSettings(inbound Inbound, keys []string) bool {
	if inbound.ProtocolFields != nil {
		for _, key := range keys {
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
