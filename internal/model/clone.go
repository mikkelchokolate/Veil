package model

// CloneProtocolFields returns a deep copy of a protocol-fields map. Values are
// decoded JSON shapes (map[string]any, []any, scalars) plus the common typed
// containers the panel stores; nested mutable containers are cloned so a
// mutation on the copy can never alias into the source.
func CloneProtocolFields(pf map[string]any) map[string]any {
	if pf == nil {
		return nil
	}
	out := make(map[string]any, len(pf))
	for k, v := range pf {
		out[k] = DeepCloneValue(v)
	}
	return out
}

// DeepCloneValue deep-copies the mutable container shapes that can appear in
// protocol fields; immutable scalars are returned unchanged.
func DeepCloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return CloneProtocolFields(t)
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = DeepCloneValue(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, len(t))
		for i, item := range t {
			out[i] = CloneProtocolFields(item)
		}
		return out
	case []string:
		return append([]string(nil), t...)
	case map[string]string:
		out := make(map[string]string, len(t))
		for k, item := range t {
			out[k] = item
		}
		return out
	default:
		return v
	}
}

// CloneInbound returns a deep copy of a single inbound: profiles, runtime
// credentials, and the full protocol-fields tree are detached from the source.
func CloneInbound(in Inbound) Inbound {
	out := in
	out.Profiles = append([]ClientProfile(nil), in.Profiles...)
	out.ProtocolFields = CloneProtocolFields(in.ProtocolFields)
	out.RuntimeCredentials = append([]RuntimeCredential(nil), in.RuntimeCredentials...)
	return out
}
