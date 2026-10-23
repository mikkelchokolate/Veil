package client

import "github.com/mikkelchokolate/Veil/internal/model"

// SnapshotRows converts repository rows into the immutable revision snapshot
// model. It is the single converter for every revision writer: the API
// mutation path, the state-file lifecycle path and the CLI state-commit path
// (admin, install, repair, key rotation). Two hand-copied converters used to
// exist, and the state-commit one silently dropped ipLimit, so a rollback to a
// CLI-committed revision restored every client as IP-unlimited.
func SnapshotRows(
	clients []Client, bindings []Binding, credentials []Credential,
) ([]model.ClientSnapshot, []model.BindingSnapshot, []model.CredentialSnapshot) {
	clientRows := make([]model.ClientSnapshot, 0, len(clients))
	for _, c := range clients {
		clientRows = append(clientRows, model.ClientSnapshot{
			ID: c.ID, Name: c.Name, Email: c.Email, Enabled: c.Enabled,
			GroupID: c.GroupID, QuotaBytes: c.QuotaBytes, QuotaResetPolicy: c.QuotaResetPolicy,
			QuotaResetAt: c.QuotaResetAt, ExpiresAt: c.ExpiresAt,
			DeviceLimit: c.DeviceLimit, IPLimit: c.IPLimit,
			Notes: c.Notes, Depleted: c.Depleted,
			CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Version: c.Version,
		})
	}
	bindingRows := make([]model.BindingSnapshot, 0, len(bindings))
	for _, b := range bindings {
		bindingRows = append(bindingRows, model.BindingSnapshot{
			ID: b.ID, ClientID: b.ClientID, InboundID: b.InboundID, RuntimeIdentity: b.RuntimeIdentity,
			Enabled: b.Enabled, ProtocolSettings: b.ProtocolSettings,
			CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, Version: b.Version,
		})
	}
	credentialRows := make([]model.CredentialSnapshot, 0, len(credentials))
	for _, k := range credentials {
		credentialRows = append(credentialRows, model.CredentialSnapshot{
			ID: k.ID, BindingID: k.BindingID, Kind: k.Kind, EncryptedValue: k.EncryptedValue,
			KeyVersion: k.KeyVersion, CredentialVersion: k.CredentialVersion,
			CreatedAt: k.CreatedAt, RotatedAt: k.RotatedAt,
		})
	}
	return clientRows, bindingRows, credentialRows
}

// RowsFromSnapshot is the inverse of SnapshotRows: it rebuilds repository rows
// from an immutable revision snapshot for rollback. Credential material is
// copied, so the returned rows never alias the snapshot's byte slices.
func RowsFromSnapshot(
	clients []model.ClientSnapshot, bindings []model.BindingSnapshot, credentials []model.CredentialSnapshot,
) ([]Client, []Binding, []Credential) {
	clientRows := make([]Client, 0, len(clients))
	for _, c := range clients {
		clientRows = append(clientRows, Client{
			ID: c.ID, Name: c.Name, Email: c.Email, Enabled: c.Enabled,
			GroupID: c.GroupID, QuotaBytes: c.QuotaBytes, QuotaResetPolicy: c.QuotaResetPolicy,
			QuotaResetAt: c.QuotaResetAt, ExpiresAt: c.ExpiresAt,
			DeviceLimit: c.DeviceLimit, IPLimit: c.IPLimit,
			Notes: c.Notes, Depleted: c.Depleted,
			CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Version: c.Version,
		})
	}
	bindingRows := make([]Binding, 0, len(bindings))
	for _, b := range bindings {
		bindingRows = append(bindingRows, Binding{
			ID: b.ID, ClientID: b.ClientID, InboundID: b.InboundID, RuntimeIdentity: b.RuntimeIdentity,
			Enabled: b.Enabled, ProtocolSettings: b.ProtocolSettings,
			CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, Version: b.Version,
		})
	}
	credentialRows := make([]Credential, 0, len(credentials))
	for _, k := range credentials {
		credentialRows = append(credentialRows, Credential{
			ID: k.ID, BindingID: k.BindingID, Kind: k.Kind,
			EncryptedValue: append([]byte(nil), k.EncryptedValue...),
			KeyVersion:     k.KeyVersion, CredentialVersion: k.CredentialVersion,
			CreatedAt: k.CreatedAt, RotatedAt: k.RotatedAt,
		})
	}
	return clientRows, bindingRows, credentialRows
}
