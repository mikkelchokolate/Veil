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
// for retained client IDs survive; clients and bindings absent from the
// selected revision are removed with the database's explicit cascade policy.
//
// Security state is monotonic (#1099):
//   - An entity deleted after the snapshot was taken is never resurrected —
//     only rows that still exist live are updated.
//   - A disable, depletion, or expiry recorded after the snapshot is never
//     undone: enabled merges as AND, depleted as OR, and expires_at keeps the
//     earlier non-zero value.
//   - A rotated runtime identity stays rotated.
//   - Credential rows are never deleted or re-activated here. Retained
//     bindings keep their live rows — active values and revoked tombstones
//     alike — so a rollback cannot resurrect a revoked or superseded
//     credential. A snapshot credential is only inserted when its binding is
//     retained AND the row is genuinely absent (no tombstone, no other active
//     credential of the same kind).
//
// Retained clients and bindings receive a new monotonic version strictly
// greater than both the live row and the archived snapshot, so pre-rollback
// optimistic-lock tokens cannot be reused.
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
		keep = append(keep, item.ID)
		if item.QuotaResetPolicy == "" {
			item.QuotaResetPolicy = ResetNever
		}
		live, err := liveClientRow(tx, item.ID)
		if errors.Is(err, sql.ErrNoRows) {
			// Deleted after the snapshot — the deletion stays (#1099).
			continue
		}
		if err != nil {
			return fmt.Errorf("client: read live client %s: %w", item.ID, err)
		}
		item.Enabled = item.Enabled && live.Enabled
		item.Depleted = item.Depleted || live.Depleted
		item.ExpiresAt = earlierExpiry(item.ExpiresAt, live.ExpiresAt)
		item.CreatedAt = live.CreatedAt
		item.UpdatedAt = now
		version, err := nextRetainedVersion(tx, "clients", item.ID, item.Version)
		if err != nil {
			return fmt.Errorf("client: restore snapshot client version %s: %w", item.ID, err)
		}
		item.Version = version
		if _, err := tx.Exec(`UPDATE clients SET name=?, email=?, enabled=?, group_id=?,
    quota_bytes=?, quota_reset_policy=?, quota_reset_at=?, expires_at=?, device_limit=?,
    notes=?, depleted=?, updated_at=?, version=? WHERE id=?`,
			item.Name, item.Email, boolToInt(item.Enabled), item.GroupID,
			item.QuotaBytes, item.QuotaResetPolicy, item.QuotaResetAt, item.ExpiresAt,
			item.DeviceLimit, item.Notes, boolToInt(item.Depleted),
			item.UpdatedAt, item.Version, item.ID); err != nil {
			return fmt.Errorf("client: restore snapshot client %s: %w", item.ID, err)
		}
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
	// Removing out-of-snapshot bindings first frees their (client_id,
	// inbound_id) slots for bindings the snapshot moves back onto them.
	if err := deleteBindingsOutsideSnapshot(tx, keepBindings); err != nil {
		return err
	}
	for _, item := range bindings {
		live, err := liveBindingRow(tx, item.ID)
		if errors.Is(err, sql.ErrNoRows) {
			// Deleted after the snapshot — the deletion stays (#1099).
			continue
		}
		if err != nil {
			return fmt.Errorf("client: read live binding %s: %w", item.ID, err)
		}
		if item.RuntimeIdentity == "" {
			item.RuntimeIdentity = GenerateRuntimeIdentity(item.ID)
		}
		if item.ProtocolSettings == "" {
			item.ProtocolSettings = "{}"
		}
		// The snapshot's parent client may itself be deleted post-snapshot;
		// keep the binding on its live parent in that case.
		if !clientRowExists(tx, item.ClientID) {
			item.ClientID = live.ClientID
		}
		// A rotated runtime identity is security material: keep the live one.
		if live.RuntimeIdentity != "" {
			item.RuntimeIdentity = live.RuntimeIdentity
		}
		item.Enabled = item.Enabled && live.Enabled
		item.CreatedAt = live.CreatedAt
		item.UpdatedAt = now
		version, err := nextRetainedVersion(tx, "client_bindings", item.ID, item.Version)
		if err != nil {
			return fmt.Errorf("client: restore snapshot binding version %s: %w", item.ID, err)
		}
		item.Version = version
		if _, err := tx.Exec(`UPDATE client_bindings SET client_id=?, inbound_id=?, runtime_identity=?,
    enabled=?, protocol_settings=?, updated_at=?, version=? WHERE id=?`,
			item.ClientID, item.InboundID, item.RuntimeIdentity,
			boolToInt(item.Enabled), item.ProtocolSettings, item.UpdatedAt,
			item.Version, item.ID); err != nil {
			return fmt.Errorf("client: restore snapshot binding %s: %w", item.ID, err)
		}
	}

	return restoreSnapshotCredentials(tx, credentials)
}

type liveClientSecurity struct {
	Enabled   bool
	Depleted  bool
	ExpiresAt *int64
	CreatedAt int64
}

func liveClientRow(tx *Tx, id string) (liveClientSecurity, error) {
	var row liveClientSecurity
	var enabled, depleted int
	err := tx.QueryRow(`SELECT enabled, depleted, expires_at, created_at FROM clients WHERE id=?`, id).
		Scan(&enabled, &depleted, &row.ExpiresAt, &row.CreatedAt)
	row.Enabled = enabled != 0
	row.Depleted = depleted != 0
	return row, err
}

type liveBindingSecurity struct {
	ClientID        string
	RuntimeIdentity string
	Enabled         bool
	CreatedAt       int64
}

func liveBindingRow(tx *Tx, id string) (liveBindingSecurity, error) {
	var row liveBindingSecurity
	var enabled int
	err := tx.QueryRow(`SELECT client_id, runtime_identity, enabled, created_at FROM client_bindings WHERE id=?`, id).
		Scan(&row.ClientID, &row.RuntimeIdentity, &enabled, &row.CreatedAt)
	row.Enabled = enabled != 0
	return row, err
}

func clientRowExists(tx *Tx, id string) bool {
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM clients WHERE id=?`, id).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// earlierExpiry keeps the earlier non-zero expiry of the snapshot and live
// values — a rollback may shorten or restore an expiry, never extend past or
// remove one set later (#1099).
func earlierExpiry(snapshot, live *int64) *int64 {
	if snapshot == nil {
		return live
	}
	if live == nil {
		return snapshot
	}
	if *live < *snapshot {
		return live
	}
	return snapshot
}

// restoreSnapshotCredentials inserts only snapshot credentials that are
// genuinely absent while their binding is retained — and never over a revoked
// tombstone or another active credential of the same kind.
func restoreSnapshotCredentials(tx *Tx, credentials []Credential) error {
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
  SELECT ?,?,?,?,?,?,?,?,NULL
  WHERE EXISTS (SELECT 1 FROM client_bindings WHERE id=?)
    AND NOT EXISTS (SELECT 1 FROM client_credentials WHERE id=?)
    AND NOT EXISTS (SELECT 1 FROM client_credentials WHERE binding_id=? AND kind=? AND revoked_at IS NULL)`,
			item.ID, item.BindingID, item.Kind, item.EncryptedValue,
			item.KeyVersion, item.CredentialVersion, item.CreatedAt, item.RotatedAt,
			item.BindingID, item.ID, item.BindingID, item.Kind); err != nil {
			return fmt.Errorf("client: restore snapshot credential %s: %w", item.ID, err)
		}
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
