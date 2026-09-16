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
