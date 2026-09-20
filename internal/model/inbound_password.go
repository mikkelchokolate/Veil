package model

import "strings"

// EffectiveInboundPassword resolves the inbound-level fallback password using
// the dynamic-form precedence: a non-empty protocolFields["password"] wins
// over the legacy flat field. The winning value is returned byte-for-byte so
// the rendered server config and the exported client config carry identical
// credential bytes (audit #311); a value that trims to empty counts as unset.
func EffectiveInboundPassword(inbound Inbound) string {
	if raw, ok := inbound.ProtocolFields["password"].(string); ok && strings.TrimSpace(raw) != "" {
		return raw
	}
	return inbound.Password
}

// EffectiveProtocolPassword resolves a protocol-scoped inbound password using
// the precedence every consumer (server renderers, validators, client export)
// must agree on: an explicitly set Inbound.Password wins byte-for-byte, then
// the inbound dynamic field, then the legacy flat inbound field, then the
// settings dynamic field, and finally the legacy settings field. A value that
// trims to empty counts as unset at the inbound-password step so it can never
// shadow a dynamic or settings-level credential; non-empty winners are
// preserved byte-for-byte (audit #311/#331).
func EffectiveProtocolPassword(inbound Inbound, settings Settings, key, inboundFallback, settingsFallback string) string {
	password := inbound.Password
	if strings.TrimSpace(password) == "" {
		password = protocolFieldTrimmed(inbound.ProtocolFields, key)
	}
	if password == "" {
		password = inboundFallback
	}
	if password == "" {
		password = protocolFieldTrimmed(settings.ProtocolFields, key)
	}
	if password == "" {
		password = settingsFallback
	}
	return password
}

// EffectiveProtocolString resolves a dynamic protocol string field with the
// same inbound-then-settings precedence: the inbound dynamic field, the
// legacy flat inbound field, the settings dynamic field, and finally the
// legacy flat settings field. Dynamic map values are trimmed; flat fields
// pass through byte-for-byte. Sharing one resolver keeps server renderers and
// client export on identical bytes (audit #331).
func EffectiveProtocolString(inbound Inbound, settings Settings, key, inboundFallback, settingsFallback string) string {
	value := protocolFieldTrimmed(inbound.ProtocolFields, key)
	if value == "" {
		value = inboundFallback
	}
	if value == "" {
		value = protocolFieldTrimmed(settings.ProtocolFields, key)
	}
	if value == "" {
		value = settingsFallback
	}
	return value
}

// protocolFieldTrimmed returns a string ProtocolFields value trimmed of
// surrounding whitespace, or "" when the key is missing or not a string.
func protocolFieldTrimmed(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, ok := m[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}
