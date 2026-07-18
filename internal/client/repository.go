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

// Repository provides parameterized SQL access to clients, bindings, and
// credentials. All writes that span multiple rows run in a transaction.
type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Create(c Client) (Client, error) {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.QuotaResetPolicy == "" {
		c.QuotaResetPolicy = ResetNever
	}
	now := nowUnix()
	c.CreatedAt, c.UpdatedAt, c.Version = now, now, 1
	_, err := r.db.Exec(`INSERT INTO clients
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

func (r *Repository) Get(id string) (Client, error) {
	row := r.db.QueryRow(`SELECT id, name, email, enabled, group_id, quota_bytes, quota_reset_policy,
	  quota_reset_at, expires_at, device_limit, notes, depleted, created_at, updated_at, version
	  FROM clients WHERE id=?`, id)
	return scanClient(row)
}

// Update applies an optimistic-locking update: it succeeds only when the row's
// current version equals wantVersion, bumping it on success.
func (r *Repository) Update(c Client, wantVersion int) (Client, error) {
	c.UpdatedAt = nowUnix()
	res, err := r.db.Exec(`UPDATE clients SET name=?, email=?, enabled=?, group_id=?, quota_bytes=?,
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
		if _, err := r.Get(c.ID); err != nil {
			return Client{}, ErrNotFound
		}
		return Client{}, ErrVersionConflict
	}
	return r.Get(c.ID)
}

func (r *Repository) Delete(id string) error {
	res, err := r.db.Exec(`DELETE FROM clients WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("client: delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDepleted flips the depleted flag without a version check (reconciler use).
func (r *Repository) SetDepleted(id string, depleted bool) error {
	v := 0
	if depleted {
		v = 1
	}
	_, err := r.db.Exec(`UPDATE clients SET depleted=?, updated_at=? WHERE id=?`, v, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("client: set depleted: %w", err)
	}
	return nil
}

func (r *Repository) List(f ListFilter) ([]Client, int, error) {
	where, args := buildWhere(f)
	var total int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM clients`+where, args...).Scan(&total); err != nil {
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
	q := `SELECT id, name, email, enabled, group_id, quota_bytes, quota_reset_policy, quota_reset_at,
	  expires_at, device_limit, notes, depleted, created_at, updated_at, version
	  FROM clients` + where + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	args = append(args, size, (page-1)*size)
	rows, err := r.db.Query(q, args...)
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
func (r *Repository) OrphanClients() ([]Client, error) {
	rows, err := r.db.Query(`SELECT id, name, email, enabled, group_id, quota_bytes, quota_reset_policy,
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

func (r *Repository) CreateBinding(b Binding) (Binding, error) {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	now := nowUnix()
	b.CreatedAt, b.UpdatedAt, b.Version = now, now, 1
	if b.ProtocolSettings == "" {
		b.ProtocolSettings = "{}"
	}
	_, err := r.db.Exec(`INSERT INTO client_bindings
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

func (r *Repository) GetBinding(id string) (Binding, error) {
	row := r.db.QueryRow(`SELECT id, client_id, inbound_id, enabled, protocol_settings, created_at, updated_at, version
	  FROM client_bindings WHERE id=?`, id)
	return scanBinding(row)
}

func (r *Repository) BindingsForClient(clientID string) ([]Binding, error) {
	rows, err := r.db.Query(`SELECT id, client_id, inbound_id, enabled, protocol_settings, created_at, updated_at, version
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

func (r *Repository) UpdateBinding(b Binding, wantVersion int) (Binding, error) {
	b.UpdatedAt = nowUnix()
	res, err := r.db.Exec(`UPDATE client_bindings SET enabled=?, protocol_settings=?, updated_at=?, version=version+1
	  WHERE id=? AND version=?`, boolToInt(b.Enabled), b.ProtocolSettings, b.UpdatedAt, b.ID, wantVersion)
	if err != nil {
		return Binding{}, fmt.Errorf("client: update binding: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := r.GetBinding(b.ID); err != nil {
			return Binding{}, ErrNotFound
		}
		return Binding{}, ErrVersionConflict
	}
	return r.GetBinding(b.ID)
}

func (r *Repository) DeleteBinding(id string) error {
	res, err := r.db.Exec(`DELETE FROM client_bindings WHERE id=?`, id)
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
func (r *Repository) DeleteBindingsForInbound(inboundID string) (int, error) {
	res, err := r.db.Exec(`DELETE FROM client_bindings WHERE inbound_id=?`, inboundID)
	if err != nil {
		return 0, fmt.Errorf("client: delete bindings for inbound: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
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
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}
