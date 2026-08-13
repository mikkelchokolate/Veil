package api

import (
	"github.com/mikkelchokolate/Veil/internal/model"
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
	// Legacy client profiles embed per-profile credentials; they are encrypted
	// at rest and must never be disclosed to API readers (viewer role included).
	if len(redacted.Profiles) > 0 {
		redacted.Profiles = make([]model.ClientProfile, len(inbound.Profiles))
		copy(redacted.Profiles, inbound.Profiles)
		for i := range redacted.Profiles {
			redacted.Profiles[i].Password = disclosure.Redact(redacted.Profiles[i].Password)
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

// preserveRedactedInbound returns a copy of update where password-typed fields
// that carry the redaction sentinel "[REDACTED]" are replaced with the current
// stored value. This is the inbound-side mirror of the settings un-redaction
// path: a PUT from the panel echoes the redacted GET representation, and saving
// it verbatim would silently replace live credentials with the sentinel.
func preserveRedactedInbound(update Inbound, current Inbound) Inbound {
	preserved := update
	disclosure := veilsettings.NewCredentialDisclosure()
	preserved.Password = disclosure.PreserveRedacted(update.Password, current.Password)
	preserved.NaivePassword = disclosure.PreserveRedacted(update.NaivePassword, current.NaivePassword)
	preserved.Hysteria2Password = disclosure.PreserveRedacted(update.Hysteria2Password, current.Hysteria2Password)
	if update.ProtocolFields != nil {
		preserved.ProtocolFields = make(map[string]any, len(update.ProtocolFields))
		for k, v := range update.ProtocolFields {
			preserved.ProtocolFields[k] = v
		}
		// ProtocolFields password keys mirror the flat credential fields, so
		// fall back to the flat value when the map key is absent on the current
		// record (the panel may echo only one representation).
		flatFallback := map[string]string{
			"password":          current.Password,
			"naivePassword":     current.NaivePassword,
			"hysteria2Password": current.Hysteria2Password,
		}
		for _, key := range []string{"password", "naivePassword", "hysteria2Password"} {
			if v, ok := preserved.ProtocolFields[key]; ok {
				if s, ok := v.(string); ok {
					currentValue, ok := current.ProtocolFields[key].(string)
					if !ok || currentValue == "" {
						currentValue = flatFallback[key]
					}
					preserved.ProtocolFields[key] = disclosure.PreserveRedacted(s, currentValue)
				}
			}
		}
	}
	// Per-profile credentials are echoed back from the redacted GET
	// representation on save; restore the stored values so the sentinel is
	// never persisted as a live credential.
	if update.Profiles != nil && current.Profiles != nil {
		preserved.Profiles = make([]model.ClientProfile, len(update.Profiles))
		for i := range update.Profiles {
			preserved.Profiles[i] = update.Profiles[i]
			if i < len(current.Profiles) {
				preserved.Profiles[i].Password = disclosure.PreserveRedacted(update.Profiles[i].Password, current.Profiles[i].Password)
			} else {
				preserved.Profiles[i].Password = disclosure.PreserveRedacted(update.Profiles[i].Password, "")
			}
		}
	}
	return preserved
}
