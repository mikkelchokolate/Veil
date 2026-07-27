package client

import (
	"fmt"
	"strings"
	"time"
)

// ReplaceSnapshotTx replaces the normalized desired client configuration from
// an immutable revision inside a caller-owned transaction. Tokens and traffic
// for retained client IDs survive; clients absent from the selected revision
// follow the database's explicit cascade/retention policy.
func ReplaceSnapshotTx(tx *Tx, clients []Client, bindings []Binding, credentials []Credential) error {
	if tx == nil {
		return fmt.Errorf("client: snapshot transaction is required")
	}
	now := time.Now().Unix()
	keep := make([]string, 0, len(clients))
	for _, item := range clients {
		if item.ID == "" || item.Name == "" {
			return fmt.Errorf("client: invalid client in immutable snapshot")
		}
		if item.QuotaResetPolicy == "" {
			item.QuotaResetPolicy = ResetNever
		}
		if item.CreatedAt == 0 {
			item.CreatedAt = now
		}
		if item.UpdatedAt == 0 {
			item.UpdatedAt = item.CreatedAt
		}
		if item.Version <= 0 {
			item.Version = 1
		}
		if _, err := tx.Exec(`INSERT INTO clients
  (id, name, email, enabled, group_id, quota_bytes, quota_reset_policy, quota_reset_at,
   expires_at, device_limit, notes, depleted, created_at, updated_at, version)
  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
  ON CONFLICT(id) DO UPDATE SET name=excluded.name, email=excluded.email,
    enabled=excluded.enabled, group_id=excluded.group_id, quota_bytes=excluded.quota_bytes,
    quota_reset_policy=excluded.quota_reset_policy, quota_reset_at=excluded.quota_reset_at,
    expires_at=excluded.expires_at, device_limit=excluded.device_limit, notes=excluded.notes,
    depleted=excluded.depleted, created_at=excluded.created_at,
    updated_at=excluded.updated_at, version=excluded.version`,
			item.ID, item.Name, item.Email, boolToInt(item.Enabled), item.GroupID,
			item.QuotaBytes, item.QuotaResetPolicy, item.QuotaResetAt, item.ExpiresAt,
			item.DeviceLimit, item.Notes, boolToInt(item.Depleted), item.CreatedAt,
			item.UpdatedAt, item.Version); err != nil {
			return fmt.Errorf("client: restore snapshot client %s: %w", item.ID, err)
		}
		keep = append(keep, item.ID)
	}
	if err := deleteClientsOutsideSnapshot(tx, keep); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM client_credentials`); err != nil {
		return fmt.Errorf("client: clear snapshot credentials: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM client_bindings`); err != nil {
		return fmt.Errorf("client: clear snapshot bindings: %w", err)
	}
	for _, item := range bindings {
		if item.ID == "" || item.ClientID == "" || item.InboundID == "" {
			return fmt.Errorf("client: invalid binding in immutable snapshot")
		}
		if item.CreatedAt == 0 {
			item.CreatedAt = now
		}
		if item.UpdatedAt == 0 {
			item.UpdatedAt = item.CreatedAt
		}
		if item.Version <= 0 {
			item.Version = 1
		}
		if item.RuntimeIdentity == "" {
			item.RuntimeIdentity = GenerateRuntimeIdentity(item.ID)
		}
		if _, err := tx.Exec(`INSERT INTO client_bindings
  (id, client_id, inbound_id, runtime_identity, enabled, protocol_settings, created_at, updated_at, version)
  VALUES(?,?,?,?,?,?,?,?,?)`, item.ID, item.ClientID, item.InboundID, item.RuntimeIdentity,
			boolToInt(item.Enabled), item.ProtocolSettings, item.CreatedAt, item.UpdatedAt,
			item.Version); err != nil {
			return fmt.Errorf("client: restore snapshot binding %s: %w", item.ID, err)
		}
	}
	for _, item := range credentials {
		if item.ID == "" || item.BindingID == "" || item.Kind == "" || len(item.EncryptedValue) == 0 {
			return fmt.Errorf("client: invalid credential in immutable snapshot")
		}
		if item.CreatedAt == 0 {
			item.CreatedAt = now
		}
		if item.KeyVersion <= 0 {
			item.KeyVersion = 1
		}
		if item.CredentialVersion <= 0 {
			item.CredentialVersion = 1
		}
		if _, err := tx.Exec(`INSERT INTO client_credentials
  (id, binding_id, kind, encrypted_value, key_version, credential_version, created_at, rotated_at, revoked_at)
  VALUES(?,?,?,?,?,?,?,?,NULL)`, item.ID, item.BindingID, item.Kind,
			item.EncryptedValue, item.KeyVersion, item.CredentialVersion,
			item.CreatedAt, item.RotatedAt); err != nil {
			return fmt.Errorf("client: restore snapshot credential %s: %w", item.ID, err)
		}
	}
	return nil
}

func deleteClientsOutsideSnapshot(tx *Tx, keep []string) error {
	if len(keep) == 0 {
		if _, err := tx.Exec(`DELETE FROM clients`); err != nil {
			return fmt.Errorf("client: clear clients outside snapshot: %w", err)
		}
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keep)), ",")
	args := make([]any, len(keep))
	for index := range keep {
		args[index] = keep[index]
	}
	if _, err := tx.Exec(`DELETE FROM clients WHERE id NOT IN (`+placeholders+`)`, args...); err != nil {
		return fmt.Errorf("client: delete clients outside snapshot: %w", err)
	}
	return nil
}
