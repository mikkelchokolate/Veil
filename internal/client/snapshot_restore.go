package client

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ReplaceSnapshotTx replaces the normalized desired client configuration from
// an immutable revision inside a caller-owned transaction. Tokens and traffic
// for retained client IDs survive; clients absent from the selected revision
// follow the database's explicit cascade/retention policy.
//
// Retained bindings are upserted in place so traffic_binding_cleanup does not
// fire for identities that still exist after rollback. Retained clients and
// bindings receive a new monotonic version strictly greater than both the live
// row and the archived snapshot, so pre-rollback optimistic-lock tokens cannot
// be reused.
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
		version, err := nextRetainedVersion(tx, "clients", item.ID, item.Version)
		if err != nil {
			return fmt.Errorf("client: restore snapshot client version %s: %w", item.ID, err)
		}
		item.Version = version
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

	keepBindings := make([]string, 0, len(bindings))
	for _, item := range bindings {
		if item.ID == "" || item.ClientID == "" || item.InboundID == "" {
			return fmt.Errorf("client: invalid binding in immutable snapshot")
		}
		keepBindings = append(keepBindings, item.ID)
	}
	if err := deleteBindingsOutsideSnapshot(tx, keepBindings); err != nil {
		return err
	}
	for _, item := range bindings {
		if item.CreatedAt == 0 {
			item.CreatedAt = now
		}
		if item.UpdatedAt == 0 {
			item.UpdatedAt = item.CreatedAt
		}
		if item.RuntimeIdentity == "" {
			item.RuntimeIdentity = GenerateRuntimeIdentity(item.ID)
		}
		if item.ProtocolSettings == "" {
			item.ProtocolSettings = "{}"
		}
		version, err := nextRetainedVersion(tx, "client_bindings", item.ID, item.Version)
		if err != nil {
			return fmt.Errorf("client: restore snapshot binding version %s: %w", item.ID, err)
		}
		item.Version = version
		if _, err := tx.Exec(`INSERT INTO client_bindings
  (id, client_id, inbound_id, runtime_identity, enabled, protocol_settings, created_at, updated_at, version)
  VALUES(?,?,?,?,?,?,?,?,?)
  ON CONFLICT(id) DO UPDATE SET client_id=excluded.client_id, inbound_id=excluded.inbound_id,
    runtime_identity=excluded.runtime_identity, enabled=excluded.enabled,
    protocol_settings=excluded.protocol_settings, created_at=excluded.created_at,
    updated_at=excluded.updated_at, version=excluded.version`,
			item.ID, item.ClientID, item.InboundID, item.RuntimeIdentity,
			boolToInt(item.Enabled), item.ProtocolSettings, item.CreatedAt, item.UpdatedAt,
			item.Version); err != nil {
			return fmt.Errorf("client: restore snapshot binding %s: %w", item.ID, err)
		}
	}

	if err := replaceSnapshotCredentials(tx, keepBindings, credentials); err != nil {
		return err
	}
	return nil
}

func nextRetainedVersion(tx *Tx, table, id string, snapshotVersion int) (int, error) {
	var query string
	switch table {
	case "clients":
		query = `SELECT version FROM clients WHERE id=?`
	case "client_bindings":
		query = `SELECT version FROM client_bindings WHERE id=?`
	default:
		return 0, fmt.Errorf("client: unknown snapshot version table %q", table)
	}
	var live int
	err := tx.QueryRow(query, id).Scan(&live)
	if errors.Is(err, sql.ErrNoRows) {
		if snapshotVersion <= 0 {
			return 1, nil
		}
		return snapshotVersion, nil
	}
	if err != nil {
		return 0, err
	}
	next := live + 1
	if snapshotVersion >= next {
		next = snapshotVersion + 1
	}
	return next, nil
}

func deleteClientsOutsideSnapshot(tx *Tx, keep []string) error {
	return deleteIDsOutsideSnapshot(tx, "clients", keep, "client: delete clients outside snapshot")
}

func deleteBindingsOutsideSnapshot(tx *Tx, keep []string) error {
	return deleteIDsOutsideSnapshot(tx, "client_bindings", keep, "client: delete bindings outside snapshot")
}

func deleteIDsOutsideSnapshot(tx *Tx, table string, keep []string, op string) error {
	switch table {
	case "clients", "client_bindings":
	default:
		return fmt.Errorf("%s: unknown table", op)
	}
	if len(keep) == 0 {
		if _, err := tx.Exec(`DELETE FROM ` + table); err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keep)), ",")
	args := make([]any, len(keep))
	for index := range keep {
		args[index] = keep[index]
	}
	if _, err := tx.Exec(`DELETE FROM `+table+` WHERE id NOT IN (`+placeholders+`)`, args...); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func replaceSnapshotCredentials(tx *Tx, keepBindings []string, credentials []Credential) error {
	if len(keepBindings) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keepBindings)), ",")
		args := make([]any, len(keepBindings))
		for index := range keepBindings {
			args[index] = keepBindings[index]
		}
		if _, err := tx.Exec(`DELETE FROM client_credentials WHERE binding_id IN (`+placeholders+`)`, args...); err != nil {
			return fmt.Errorf("client: replace snapshot credentials: %w", err)
		}
	}
	now := time.Now().Unix()
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
