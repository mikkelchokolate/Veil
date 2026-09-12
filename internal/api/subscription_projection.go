package api

import "github.com/mikkelchokolate/Veil/internal/model"

func cloneAppliedProjection(snapshot managementSnapshot) managementSnapshot {
	cloned := snapshot
	cloned.Settings.ProtocolFields = cloneProtocolFieldMap(snapshot.Settings.ProtocolFields)
	if snapshot.Settings.FirewallManagement != nil {
		value := *snapshot.Settings.FirewallManagement
		cloned.Settings.FirewallManagement = &value
	}
	cloned.Inbounds = cloneProjectionInbounds(snapshot.Inbounds)
	cloned.Rules = append([]model.RoutingRule(nil), snapshot.Rules...)
	cloned.Users = append([]model.User(nil), snapshot.Users...)
	cloned.Clients = cloneProjectionClients(snapshot.Clients)
	cloned.Bindings = append([]model.BindingSnapshot(nil), snapshot.Bindings...)
	cloned.Credentials = cloneProjectionCredentials(snapshot.Credentials)
	if snapshot.RoutingSource.Files != nil {
		cloned.RoutingSource.Files = append([]model.RoutingSourceFile(nil), snapshot.RoutingSource.Files...)
	}
	if snapshot.Warp.Reserved != nil {
		cloned.Warp.Reserved = append([]int(nil), snapshot.Warp.Reserved...)
	}
	return cloned
}

func cloneProjectionInbounds(inbounds []model.Inbound) []model.Inbound {
	if inbounds == nil {
		return nil
	}
	out := make([]model.Inbound, len(inbounds))
	for i, inbound := range inbounds {
		out[i] = inbound
		out[i].ProtocolFields = cloneProtocolFieldMap(inbound.ProtocolFields)
		out[i].Profiles = append([]model.ClientProfile(nil), inbound.Profiles...)
		if inbound.RuntimeCredentials != nil {
			out[i].RuntimeCredentials = append([]model.RuntimeCredential(nil), inbound.RuntimeCredentials...)
		}
	}
	return out
}

func cloneProjectionClients(clients []model.ClientSnapshot) []model.ClientSnapshot {
	if clients == nil {
		return nil
	}
	out := make([]model.ClientSnapshot, len(clients))
	for i, row := range clients {
		out[i] = row
		out[i].Email = cloneStringPointer(row.Email)
		out[i].GroupID = cloneStringPointer(row.GroupID)
		out[i].QuotaBytes = cloneInt64Pointer(row.QuotaBytes)
		out[i].QuotaResetAt = cloneInt64Pointer(row.QuotaResetAt)
		out[i].ExpiresAt = cloneInt64Pointer(row.ExpiresAt)
		out[i].DeviceLimit = cloneIntPointer(row.DeviceLimit)
	}
	return out
}

func cloneProjectionCredentials(credentials []model.CredentialSnapshot) []model.CredentialSnapshot {
	if credentials == nil {
		return nil
	}
	out := make([]model.CredentialSnapshot, len(credentials))
	for i, credential := range credentials {
		out[i] = credential
		if credential.EncryptedValue != nil {
			out[i].EncryptedValue = append([]byte(nil), credential.EncryptedValue...)
		}
		if credential.RotatedAt != nil {
			rotated := *credential.RotatedAt
			out[i].RotatedAt = &rotated
		}
	}
	return out
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

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
