package client

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors surfaced to the service/API layer for correct HTTP mapping.
var (
	ErrNotFound         = errors.New("client: not found")
	ErrVersionConflict  = errors.New("client: version conflict")
	ErrDuplicateBinding = errors.New("client: binding already exists for client+inbound")
)

// ListFilter drives SQL-level pagination and filtering for the client list.
type ListFilter struct {
	Page          int
	PageSize      int
	Search        string // matches name or email (case-insensitive substring)
	Status        string // "enabled" | "disabled" | "" (all)
	InboundID     string
	GroupID       string
	ExpiresBefore *int64
	ExpiresAfter  *int64
	QuotaState    string // "depleted" | "" (all)
	Sort          string // name | created | expires (default created desc)
}

// DBTX is the database/sql surface shared by *sql.DB and *sql.Tx so every
// repository operation can run either standalone (Repository) or inside a
// caller-managed transaction (Tx). This is what allows a client mutation, the
// desired-revision bump, and the immutable snapshot to commit atomically.
type DBTX interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// queries holds every SQL operation of the client domain. It is embedded by
// both Repository (autocommit) and Tx (transactional), so the SQL exists
// exactly once and both paths execute identical statements.
type queries struct{ q DBTX }

// Repository provides parameterized SQL access to clients, bindings, and
// credentials. All writes that span multiple rows run in a transaction.
type Repository struct {
	queries
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository { return &Repository{queries: queries{q: db}, db: db} }

// Tx is a transactional view of the repository. All methods execute within
// the bound *sql.Tx so a multi-entity mutation (client + bindings +
// credentials + revision snapshot) commits or rolls back atomically.
type Tx struct {
	queries
	tx *sql.Tx
}

// BeginTx opens a caller-managed transaction. The caller MUST call exactly one
// of Commit or Rollback. Use this (instead of WithTx) when the transaction
// must interleave client writes with other stores on the same database — e.g.
// the desired-revision bump and immutable snapshot recorded by the API layer.
func (r *Repository) BeginTx() (*Tx, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("client: begin tx: %w", err)
	}
	return &Tx{queries: queries{q: tx}, tx: tx}, nil
}

// Commit commits the underlying transaction.
func (t *Tx) Commit() error {
	if err := t.tx.Commit(); err != nil {
		return fmt.Errorf("client: commit tx: %w", err)
	}
	return nil
}

// Rollback rolls back the underlying transaction.
func (t *Tx) Rollback() error { return t.tx.Rollback() }

// Exec and QueryRow expose the underlying transaction so Tx satisfies the
// narrow DBTX interfaces of the apply stores (revision bump + snapshot save)
// that join the same atomic commit.
func (t *Tx) Exec(query string, args ...any) (sql.Result, error) { return t.q.Exec(query, args...) }

// Query exposes the underlying transaction for cross-store operations that
// share the client DBTX contract.
func (t *Tx) Query(query string, args ...any) (*sql.Rows, error) { return t.q.Query(query, args...) }

// QueryRow exposes the underlying transaction; see Exec.
func (t *Tx) QueryRow(query string, args ...any) *sql.Row { return t.q.QueryRow(query, args...) }

var validSavepointName = func(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// Savepoint creates a named savepoint so part of the transaction (e.g. one
// client inside a bulk operation) can be rolled back without aborting the
// rest. Names are restricted to [A-Za-z0-9_].
func (t *Tx) Savepoint(name string) error {
	if !validSavepointName(name) {
		return fmt.Errorf("client: invalid savepoint name %q", name)
	}
	_, err := t.q.Exec(`SAVEPOINT "` + name + `"`)
	return err
}

// ReleaseSavepoint releases a savepoint after the nested work succeeded.
func (t *Tx) ReleaseSavepoint(name string) error {
	if !validSavepointName(name) {
		return fmt.Errorf("client: invalid savepoint name %q", name)
	}
	_, err := t.q.Exec(`RELEASE SAVEPOINT "` + name + `"`)
	return err
}

// RollbackToSavepoint undoes everything since the savepoint and releases it,
// leaving the outer transaction usable.
func (t *Tx) RollbackToSavepoint(name string) error {
	if !validSavepointName(name) {
		return fmt.Errorf("client: invalid savepoint name %q", name)
	}
	if _, err := t.q.Exec(`ROLLBACK TO SAVEPOINT "` + name + `"`); err != nil {
		return err
	}
	_, err := t.q.Exec(`RELEASE SAVEPOINT "` + name + `"`)
	return err
}

// WithTx runs fn inside a single SQL transaction. If fn returns an error the
// transaction is rolled back; otherwise it is committed. This is the ONLY
// supported way to perform a logical Client mutation that spans clients,
// bindings, and credentials — compensating deletes across public service
// methods are not a substitute for a real ROLLBACK.
func (r *Repository) WithTx(fn func(tx *Tx) error) error {
	tx, err := r.BeginTx()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (q queries) CreateClient(c Client) (Client, error) {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.QuotaResetPolicy == "" {
		c.QuotaResetPolicy = ResetNever
	}
	now := nowUnix()
	c.CreatedAt, c.UpdatedAt, c.Version = now, now, 1
	_, err := q.q.Exec(`INSERT INTO clients
  (id, name, email, enabled, group_id, quota_bytes, quota_reset_policy, quota_reset_at,
   expires_at, device_limit, notes, depleted, created_at, updated_at, version)
  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.Name, c.Email, boolToInt(c.Enabled), c.GroupID, c.QuotaBytes, c.QuotaResetPolicy,
		c.QuotaResetAt, c.ExpiresAt, c.DeviceLimit, c.Notes, boolToInt(c.Depleted),
		c.CreatedAt, c.UpdatedAt, c.Version)
	if err != nil {
		return Client{}, fmt.Errorf("client: create: %w", err)
	}
	return c, nil
}

// Create is the autocommit single-client insert (legacy path).
func (q queries) Create(c Client) (Client, error) { return q.CreateClient(c) }

func (q queries) Get(id string) (Client, error) {
	row := q.q.QueryRow(`SELECT id, name, email, enabled, group_id, quota_bytes, quota_reset_policy,
  quota_reset_at, expires_at, device_limit, notes, depleted, created_at, updated_at, version
  FROM clients WHERE id=?`, id)
	return scanClient(row)
}

// Update applies an optimistic-locking update: it succeeds only when the row's
// current version equals wantVersion, bumping it on success.
func (q queries) Update(c Client, wantVersion int) (Client, error) {
	c.UpdatedAt = nowUnix()
	res, err := q.q.Exec(`UPDATE clients SET name=?, email=?, enabled=?, group_id=?, quota_bytes=?,
  quota_reset_policy=?, quota_reset_at=?, expires_at=?, device_limit=?, notes=?, depleted=?,
  updated_at=?, version=version+1
  WHERE id=? AND version=?`,
		c.Name, c.Email, boolToInt(c.Enabled), c.GroupID, c.QuotaBytes, c.QuotaResetPolicy,
		c.QuotaResetAt, c.ExpiresAt, c.DeviceLimit, c.Notes, boolToInt(c.Depleted),
		c.UpdatedAt, c.ID, wantVersion)
	if err != nil {
		return Client{}, fmt.Errorf("client: update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := q.Get(c.ID); err != nil {
			return Client{}, ErrNotFound
		}
		return Client{}, ErrVersionConflict
	}
	return q.Get(c.ID)
}

func (q queries) Delete(id string) error {
	res, err := q.q.Exec(`DELETE FROM clients WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("client: delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDepleted flips the depleted flag without a version check (reconciler use).
func (q queries) SetDepleted(id string, depleted bool) error {
	v := 0
	if depleted {
		v = 1
	}
	res, err := q.q.Exec(`UPDATE clients SET depleted=?, updated_at=? WHERE id=?`, v, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("client: set depleted: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (q queries) List(f ListFilter) ([]Client, int, error) {
	where, args := buildWhere(f)
	var total int
	if err := q.q.QueryRow(`SELECT COUNT(*) FROM clients`+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("client: count: %w", err)
	}
	page, size := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 25
	}
	order := "created_at DESC"
	switch f.Sort {
	case "name":
		order = "name COLLATE NOCASE ASC"
	case "created":
		order = "created_at DESC"
	case "expires":
		order = "expires_at IS NULL, expires_at ASC"
	}
	query := `SELECT id, name, email, enabled, group_id, quota_bytes, quota_reset_policy, quota_reset_at,
  expires_at, device_limit, notes, depleted, created_at, updated_at, version
  FROM clients` + where + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	args = append(args, size, (page-1)*size)
	rows, err := q.q.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("client: list: %w", err)
	}
	defer rows.Close()
	var out []Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

func buildWhere(f ListFilter) (string, []any) {
	var clauses []string
	var args []any
	if f.Search != "" {
		clauses = append(clauses, "(name LIKE ? OR email LIKE ?)")
		like := "%" + f.Search + "%"
		args = append(args, like, like)
	}
	switch f.Status {
	case "enabled":
		clauses = append(clauses, "enabled=1")
	case "disabled":
		clauses = append(clauses, "enabled=0")
	}
	if f.GroupID != "" {
		clauses = append(clauses, "group_id=?")
		args = append(args, f.GroupID)
	}
	if f.ExpiresBefore != nil {
		clauses = append(clauses, "expires_at IS NOT NULL AND expires_at<?")
		args = append(args, *f.ExpiresBefore)
	}
	if f.ExpiresAfter != nil {
		clauses = append(clauses, "expires_at IS NOT NULL AND expires_at>?")
		args = append(args, *f.ExpiresAfter)
	}
	if f.QuotaState == "depleted" {
		clauses = append(clauses, "depleted=1")
	}
	if f.InboundID != "" {
		clauses = append(clauses, "id IN (SELECT client_id FROM client_bindings WHERE inbound_id=?)")
		args = append(args, f.InboundID)
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// OrphanClients returns clients with no bindings.
func (q queries) OrphanClients() ([]Client, error) {
	rows, err := q.q.Query(`SELECT id, name, email, enabled, group_id, quota_bytes, quota_reset_policy,
  quota_reset_at, expires_at, device_limit, notes, depleted, created_at, updated_at, version
  FROM clients WHERE id NOT IN (SELECT DISTINCT client_id FROM client_bindings)`)
	if err != nil {
		return nil, fmt.Errorf("client: orphans: %w", err)
	}
	defer rows.Close()
	var out []Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- Bindings ---

func (q queries) CreateBinding(b Binding) (Binding, error) {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	now := nowUnix()
	b.CreatedAt, b.UpdatedAt, b.Version = now, now, 1
	if b.ProtocolSettings == "" {
		b.ProtocolSettings = "{}"
	}
	_, err := q.q.Exec(`INSERT INTO client_bindings
  (id, client_id, inbound_id, enabled, protocol_settings, created_at, updated_at, version)
  VALUES(?,?,?,?,?,?,?,?)`,
		b.ID, b.ClientID, b.InboundID, boolToInt(b.Enabled), b.ProtocolSettings,
		b.CreatedAt, b.UpdatedAt, b.Version)
	if err != nil {
		if isUniqueViolation(err) {
			return Binding{}, ErrDuplicateBinding
		}
		return Binding{}, fmt.Errorf("client: create binding: %w", err)
	}
	return b, nil
}

func (q queries) GetBinding(id string) (Binding, error) {
	row := q.q.QueryRow(`SELECT id, client_id, inbound_id, enabled, protocol_settings, created_at, updated_at, version
  FROM client_bindings WHERE id=?`, id)
	return scanBinding(row)
}

func (q queries) BindingsForClient(clientID string) ([]Binding, error) {
	rows, err := q.q.Query(`SELECT id, client_id, inbound_id, enabled, protocol_settings, created_at, updated_at, version
  FROM client_bindings WHERE client_id=? ORDER BY created_at ASC`, clientID)
	if err != nil {
		return nil, fmt.Errorf("client: bindings: %w", err)
	}
	defer rows.Close()
	var out []Binding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (q queries) UpdateBinding(b Binding, wantVersion int) (Binding, error) {
	b.UpdatedAt = nowUnix()
	res, err := q.q.Exec(`UPDATE client_bindings SET enabled=?, protocol_settings=?, updated_at=?, version=version+1
  WHERE id=? AND version=?`, boolToInt(b.Enabled), b.ProtocolSettings, b.UpdatedAt, b.ID, wantVersion)
	if err != nil {
		return Binding{}, fmt.Errorf("client: update binding: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := q.GetBinding(b.ID); err != nil {
			return Binding{}, ErrNotFound
		}
		return Binding{}, ErrVersionConflict
	}
	return q.GetBinding(b.ID)
}

func (q queries) DeleteBinding(id string) error {
	res, err := q.q.Exec(`DELETE FROM client_bindings WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("client: delete binding: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteBindingsForInbound removes all bindings for an inbound (cascade on
// inbound deletion) while leaving the clients themselves intact (orphans).
func (q queries) DeleteBindingsForInbound(inboundID string) (int, error) {
	res, err := q.q.Exec(`DELETE FROM client_bindings WHERE inbound_id=?`, inboundID)
	if err != nil {
		return 0, fmt.Errorf("client: delete bindings for inbound: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// AllClients returns every client (no pagination) for revision snapshots.
func (q queries) AllClients() ([]Client, error) {
	rows, err := q.q.Query(`SELECT id, name, email, enabled, group_id, quota_bytes, quota_reset_policy,
  quota_reset_at, expires_at, device_limit, notes, depleted, created_at, updated_at, version
  FROM clients ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("client: list all: %w", err)
	}
	defer rows.Close()
	var out []Client
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AllBindings returns every binding (no pagination) for revision snapshots.
func (q queries) AllBindings() ([]Binding, error) {
	rows, err := q.q.Query(`SELECT id, client_id, inbound_id, enabled, protocol_settings, created_at, updated_at, version
  FROM client_bindings ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("client: list all bindings: %w", err)
	}
	defer rows.Close()
	var out []Binding
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AllActiveCredentials returns every active (non-revoked) credential for
// revision snapshots. Encrypted material is included so a retry of revision N
// renders with exactly the credential that was active at revision N.
func (q queries) AllActiveCredentials() ([]Credential, error) {
	rows, err := q.q.Query(`SELECT id, binding_id, kind, encrypted_value, key_version, credential_version,
  created_at, rotated_at, revoked_at FROM client_credentials WHERE revoked_at IS NULL ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("client: list active credentials: %w", err)
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		c, err := scanCredential(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- traffic ---

// ResetTrafficForClient zeroes a client's cumulative counters and bucketed
// samples inside the current transaction.
func (q queries) ResetTrafficForClient(clientID string) error {
	if _, err := q.q.Exec(`DELETE FROM traffic_counters WHERE client_id=?`, clientID); err != nil {
		return fmt.Errorf("client: traffic reset counters: %w", err)
	}
	if _, err := q.q.Exec(`DELETE FROM traffic_samples WHERE client_id=?`, clientID); err != nil {
		return fmt.Errorf("client: traffic reset samples: %w", err)
	}
	return nil
}

// --- migration markers ---

// MigrationMarker records that a one-way data migration ran to completion.
type MigrationMarker struct {
	Key       string
	Version   int
	AppliedAt int64
	Details   string
}

// GetMigrationMarker returns the marker for a key, or (nil, nil) when the
// migration has not run yet.
func (q queries) GetMigrationMarker(key string) (*MigrationMarker, error) {
	row := q.q.QueryRow(`SELECT key, version, applied_at, details FROM migration_markers WHERE key=?`, key)
	var m MigrationMarker
	err := row.Scan(&m.Key, &m.Version, &m.AppliedAt, &m.Details)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("client: read migration marker: %w", err)
	}
	return &m, nil
}

// PutMigrationMarker records (or replaces) a migration marker.
func (q queries) PutMigrationMarker(m MigrationMarker) error {
	if m.Details == "" {
		m.Details = "{}"
	}
	_, err := q.q.Exec(`INSERT OR REPLACE INTO migration_markers(key, version, applied_at, details)
  VALUES(?,?,?,?)`, m.Key, m.Version, m.AppliedAt, m.Details)
	if err != nil {
		return fmt.Errorf("client: write migration marker: %w", err)
	}
	return nil
}

// --- helpers ---

type scanner interface{ Scan(dest ...any) error }

func scanClient(row scanner) (Client, error) {
	var c Client
	var enabled, depleted int
	err := row.Scan(&c.ID, &c.Name, &c.Email, &enabled, &c.GroupID, &c.QuotaBytes,
		&c.QuotaResetPolicy, &c.QuotaResetAt, &c.ExpiresAt, &c.DeviceLimit, &c.Notes,
		&depleted, &c.CreatedAt, &c.UpdatedAt, &c.Version)
	if err != nil {
		return Client{}, err
	}
	c.Enabled = enabled == 1
	c.Depleted = depleted == 1
	return c, nil
}

func scanBinding(row scanner) (Binding, error) {
	var b Binding
	var enabled int
	err := row.Scan(&b.ID, &b.ClientID, &b.InboundID, &enabled, &b.ProtocolSettings,
		&b.CreatedAt, &b.UpdatedAt, &b.Version)
	if err != nil {
		return Binding{}, err
	}
	b.Enabled = enabled == 1
	return b, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
