package client

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/secrets"
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
//     earlier non-zero value. Depletion is derived quota state, so it is
//     carried forward only onto a row that still has a quota; a row restored
//     without one transitions to unlimited like a live quota clear (#1350).
//     A row restored with a different quota plan (bytes, reset policy or
//     boundary) supersedes the live enforcement record bound to the old
//     plan, like a live plan change (#1355).
//   - A rotated runtime identity stays rotated.
//   - Credential rows are never deleted or re-activated here. Retained
//     bindings keep their live rows — active values and revoked tombstones
//     alike — so a rollback cannot resurrect a revoked or superseded
//     credential. That includes a binding the snapshot moves back to another
//     client: its row is re-inserted, and its live credential rows are put
//     back unchanged. A snapshot credential is only inserted when its binding
//     is retained AND the row is genuinely absent (no tombstone, no other
//     active credential of the same kind).
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
		// Depletion is derived quota state (#1350): it survives only while
		// the restored row still has a quota. Restoring "no quota" is an
		// explicit transition to unlimited, handled like a live quota clear
		// (normalizeClearedQuota + prepareQuotaClear): the flag is dropped
		// and any enforcement target bound to the old plan is superseded,
		// so neither depleted-without-quota nor a pending depleted runtime
		// target can survive the rollback.
		if item.QuotaBytes == nil {
			item.Depleted = false
			if live.hasQuotaState() {
				if err := tx.SupersedeQuotaEnforcement(item.ID); err != nil {
					return err
				}
			}
		} else {
			item.Depleted = item.Depleted || live.Depleted
			// A restored quota plan that differs from the live one (bytes,
			// reset policy or reset boundary) leaves the live enforcement
			// record bound to the old plan: its target was computed under
			// the old values. Supersede it in the same tx, as a live plan
			// change does (prepareQuotaResetTransition), so the reconciler
			// replans against the restored plan (#1355).
			if live.quotaPlanDiffers(item) {
				if err := tx.SupersedeQuotaEnforcement(item.ID); err != nil {
					return err
				}
			}
		}
		item.ExpiresAt = earlierExpiry(item.ExpiresAt, live.ExpiresAt)
		item.UpdatedAt = now
		version, err := nextRetainedVersion(tx, "clients", item.ID, item.Version)
		if err != nil {
			return fmt.Errorf("client: restore snapshot client version %s: %w", item.ID, err)
		}
		item.Version = version
		if _, err := tx.Exec(`UPDATE clients SET name=?, email=?, enabled=?, group_id=?,
    quota_bytes=?, quota_reset_policy=?, quota_reset_at=?, expires_at=?, device_limit=?,
    ip_limit=?, notes=?, depleted=?, updated_at=?, version=? WHERE id=?`,
			item.Name, item.Email, boolToInt(item.Enabled), item.GroupID,
			item.QuotaBytes, item.QuotaResetPolicy, item.QuotaResetAt, item.ExpiresAt,
			item.DeviceLimit, item.IPLimit, item.Notes, boolToInt(item.Depleted),
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
		// Identities are canonical lowercase (#1111); a restore never writes
		// a mixed-case one that hysteria2 traffic would not match (#1273).
		item.RuntimeIdentity = strings.ToLower(item.RuntimeIdentity)
		item.Enabled = item.Enabled && live.Enabled
		item.CreatedAt = live.CreatedAt
		item.UpdatedAt = now
		version, err := nextRetainedVersion(tx, "client_bindings", item.ID, item.Version)
		if err != nil {
			return fmt.Errorf("client: restore snapshot binding version %s: %w", item.ID, err)
		}
		item.Version = version
		// A binding whose live row sits under a different client must be
		// deleted and re-inserted, not updated in place: the UPDATE path
		// never fires traffic_binding_cleanup, so counters/samples keyed
		// (old client, binding) would survive and trip the next
		// foreign-key/domain integrity check at storage.Open (#1140).
		//
		// The delete also cascades into client_credentials. Those rows are
		// security state, not counters: a post-snapshot rotation and its
		// revoked tombstones must survive (#1099), or the snapshot credential
		// below would become the only active one again (#1272 item 6). Keep
		// them aside and put them back verbatim once the binding row exists.
		var existingClient string
		var keptCredentials []credentialRow
		switch err := tx.QueryRow(`SELECT client_id FROM client_bindings WHERE id=?`, item.ID).Scan(&existingClient); {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("client: read existing binding %s: %w", item.ID, err)
		case existingClient != item.ClientID:
			keptCredentials, err = bindingCredentialRows(tx, item.ID)
			if err != nil {
				return fmt.Errorf("client: read credentials of reparented binding %s: %w", item.ID, err)
			}
			if _, err := tx.Exec(`DELETE FROM client_bindings WHERE id=?`, item.ID); err != nil {
				return fmt.Errorf("client: reparent snapshot binding %s: %w", item.ID, err)
			}
		}
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
		if err := reinsertCredentialRows(tx, keptCredentials); err != nil {
			return fmt.Errorf("client: keep credentials of reparented binding %s: %w", item.ID, err)
		}
	}

	return restoreSnapshotCredentials(tx, credentials)
}

// credentialRow is a client_credentials row copied byte-for-byte, so a
// binding that is deleted and re-inserted keeps its credential history.
type credentialRow struct {
	ID                string
	BindingID         string
	Kind              string
	EncryptedValue    []byte
	KeyVersion        int
	CredentialVersion int
	CreatedAt         int64
	RotatedAt         *int64
	RevokedAt         *int64
}

func bindingCredentialRows(tx *Tx, bindingID string) ([]credentialRow, error) {
	rows, err := tx.Query(`SELECT id, binding_id, kind, encrypted_value, key_version,
  credential_version, created_at, rotated_at, revoked_at
  FROM client_credentials WHERE binding_id=? ORDER BY credential_version, id`, bindingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []credentialRow
	for rows.Next() {
		var r credentialRow
		if err := rows.Scan(&r.ID, &r.BindingID, &r.Kind, &r.EncryptedValue, &r.KeyVersion,
			&r.CredentialVersion, &r.CreatedAt, &r.RotatedAt, &r.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func reinsertCredentialRows(tx *Tx, rows []credentialRow) error {
	for _, r := range rows {
		if _, err := tx.Exec(`INSERT INTO client_credentials
  (id, binding_id, kind, encrypted_value, key_version, credential_version, created_at, rotated_at, revoked_at)
  VALUES(?,?,?,?,?,?,?,?,?)`,
			r.ID, r.BindingID, r.Kind, r.EncryptedValue, r.KeyVersion, r.CredentialVersion,
			r.CreatedAt, r.RotatedAt, r.RevokedAt); err != nil {
			return err
		}
	}
	return nil
}

type liveClientSecurity struct {
	Enabled          bool
	Depleted         bool
	ExpiresAt        *int64
	CreatedAt        int64
	QuotaBytes       *int64
	QuotaResetPolicy string
	QuotaResetAt     *int64
}

// hasQuotaState mirrors prepareQuotaClear's trigger: the live row carries a
// quota plan or derived quota state that a transition to unlimited must
// supersede.
func (row liveClientSecurity) hasQuotaState() bool {
	return row.QuotaBytes != nil || row.Depleted || row.QuotaResetAt != nil ||
		(row.QuotaResetPolicy != "" && row.QuotaResetPolicy != ResetNever)
}

// quotaPlanDiffers reports whether a restored client that still has a quota
// carries a different effective plan than the live row. It is compared by
// value, with an empty policy meaning "never" on both sides. This is
// deliberately stricter about "effective" than prepareQuotaResetTransition,
// which compares the raw policy strings: there a ""->"never" edit costs only
// a spurious replan, while here it would drop a record for the same plan.
func (row liveClientSecurity) quotaPlanDiffers(restored Client) bool {
	livePolicy, restoredPolicy := row.QuotaResetPolicy, restored.QuotaResetPolicy
	if livePolicy == "" {
		livePolicy = ResetNever
	}
	if restoredPolicy == "" {
		restoredPolicy = ResetNever
	}
	return !sameInt64Ptr(row.QuotaBytes, restored.QuotaBytes) ||
		livePolicy != restoredPolicy ||
		!sameInt64Ptr(row.QuotaResetAt, restored.QuotaResetAt)
}

func liveClientRow(tx *Tx, id string) (liveClientSecurity, error) {
	var row liveClientSecurity
	var enabled, depleted int
	err := tx.QueryRow(`SELECT enabled, depleted, expires_at, created_at, quota_bytes, quota_reset_policy, quota_reset_at FROM clients WHERE id=?`, id).
		Scan(&enabled, &depleted, &row.ExpiresAt, &row.CreatedAt, &row.QuotaBytes, &row.QuotaResetPolicy, &row.QuotaResetAt)
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
		// A non-"ve1:" value is never produced by CredentialStore; restoring
		// it would plant a live row that Reveal refuses and that makes every
		// later master-key rotation fail closed (#1283 follow-up).
		if !secrets.IsEncrypted(string(item.EncryptedValue)) {
			return fmt.Errorf("client: snapshot credential %s ciphertext is invalid", item.ID)
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
