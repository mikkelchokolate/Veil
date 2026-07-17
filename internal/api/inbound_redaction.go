package api

import (
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

// redactInbound returns a copy of inbound with sensitive fields masked for API
// responses. The caller must not rely on the returned value for persistence;
// it is intended only for serialization to clients.
func redactInbound(inbound Inbound) Inbound {
	redacted := inbound
	disclosure := veilsettings.NewCredentialDisclosure()
	redacted.Password = disclosure.Redact(redacted.Password)
	redacted.NaivePassword = disclosure.Redact(redacted.NaivePassword)
	redacted.Hysteria2Password = disclosure.Redact(redacted.Hysteria2Password)
	if redacted.ProtocolFields != nil {
		redacted.ProtocolFields = make(map[string]any, len(inbound.ProtocolFields))
		for k, v := range inbound.ProtocolFields {
			redacted.ProtocolFields[k] = v
		}
		for _, key := range []string{"password", "naivePassword", "hysteria2Password"} {
			if v, ok := redacted.ProtocolFields[key]; ok {
				if s, ok := v.(string); ok {
					redacted.ProtocolFields[key] = disclosure.Redact(s)
				}
			}
		}
	}
	return redacted
}

// redactInboundList redacts a slice of inbounds for API responses.
func redactInboundList(inbounds []Inbound) []Inbound {
	if inbounds == nil {
		return nil
	}
	redacted := make([]Inbound, len(inbounds))
	for i, in := range inbounds {
		redacted[i] = redactInbound(in)
	}
	return redacted
}
