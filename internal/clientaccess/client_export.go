package clientaccess

// CloneSettings returns a detached copy of settings, including ProtocolFields.
// Per-client export paths must not share maps with live or cached snapshots.
func CloneSettings(settings Settings) Settings {
	cloned := settings
	cloned.ProtocolFields = cloneProtocolFieldMap(settings.ProtocolFields)
	if settings.FirewallManagement != nil {
		value := *settings.FirewallManagement
		cloned.FirewallManagement = &value
	}
	return cloned
}

// MaterializeInbound copies legacy flat protocol fields into ProtocolFields
// when the dynamic map does not already carry them, so reduced per-client
// inbound snapshots still preserve the effective export contract.
func MaterializeInbound(inbound Inbound) Inbound {
	inbound = normalizeClientAccessInbound(inbound)
	fields := cloneProtocolFieldMap(inbound.ProtocolFields)
	if fields == nil {
		fields = map[string]any{}
	}
	setIfAbsent := func(key string, value any, present bool) {
		if !present {
			return
		}
		if _, ok := fields[key]; ok {
			return
		}
		fields[key] = value
	}
	setIfAbsent("hysteria2Insecure", inbound.Hysteria2Insecure, inbound.Hysteria2Insecure)
	setIfAbsent("hysteria2Password", inbound.Hysteria2Password, inbound.Hysteria2Password != "")
	setIfAbsent("naiveUsername", inbound.NaiveUsername, inbound.NaiveUsername != "")
	setIfAbsent("naivePassword", inbound.NaivePassword, inbound.NaivePassword != "")
	setIfAbsent("masqueradeURL", inbound.MasqueradeURL, inbound.MasqueradeURL != "")
	setIfAbsent("fallbackRoot", inbound.FallbackRoot, inbound.FallbackRoot != "")
	setIfAbsent("olcrtcAuth", inbound.OlcrtcAuth, inbound.OlcrtcAuth != "")
	setIfAbsent("olcrtcTransport", inbound.OlcrtcTransport, inbound.OlcrtcTransport != "")
	setIfAbsent("olcrtcRoomID", inbound.OlcrtcRoomID, inbound.OlcrtcRoomID != "")
	inbound.ProtocolFields = fields
	return inbound
}

func cloneProtocolFieldMap(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	out := make(map[string]any, len(fields))
	for key, value := range fields {
		out[key] = cloneProtocolFieldValue(value)
	}
	return out
}

func cloneProtocolFieldValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneProtocolFieldMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneProtocolFieldValue(typed[i])
		}
		return out
	default:
		return value
	}
}
