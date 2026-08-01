package client

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	tokenByteLen   = 32 // 256-bit token
	tokenPrefixLen = 8
)

// IssuedToken pairs the stored token row with the one-time plaintext. The
// plaintext is returned to the caller exactly once and never persisted.
type IssuedToken struct {
	Token     SubscriptionToken `json:"token"`
	Plaintext string            `json:"plaintext"`
	URL       string            `json:"url,omitempty"`
}

// TokenStore manages subscription tokens. Tokens are high-entropy random
// values; only their SHA-256 hash is stored so a database leak does not expose
// usable tokens.
type TokenStore struct {
	db      *sql.DB
	usageMu sync.Mutex
	usage   map[string]int64
}

func NewTokenStore(db *sql.DB) *TokenStore { return &TokenStore{db: db, usage: make(map[string]int64)} }

// Issue creates a new token for a client, returning the plaintext once.
// Optional expiry (unix seconds) bounds how long the token grants access.
func (s *TokenStore) Issue(clientID, label string, expiresAt *int64) (IssuedToken, error) {
	return s.IssueBy(clientID, label, "", expiresAt)
}

func (s *TokenStore) IssueBy(clientID, label, createdBy string, expiresAt *int64) (IssuedToken, error) {
	if err := validateTokenMetadata(label, expiresAt); err != nil {
		return IssuedToken{}, err
	}
	plaintext, err := generateToken()
	if err != nil {
		return IssuedToken{}, err
	}
	t := SubscriptionToken{
		ID:        uuid.NewString(),
		ClientID:  clientID,
		TokenHash: hashToken(plaintext),
		Prefix:    tokenPrefix(plaintext),
		Label:     label,
		CreatedBy: createdBy,
		Enabled:   true,
		ExpiresAt: expiresAt,
		CreatedAt: nowUnix(),
	}
	raw, err := s.db.Begin()
	if err != nil {
		return IssuedToken{}, err
	}
	defer raw.Rollback() //nolint:errcheck
	var parent string
	if err := raw.QueryRow(`SELECT id FROM clients WHERE id=?`, clientID).Scan(&parent); err != nil {
		if err == sql.ErrNoRows {
			return IssuedToken{}, ErrNotFound
		}
		return IssuedToken{}, err
	}
	_, err = raw.Exec(`INSERT INTO subscription_tokens
	  (id, client_id, token_hash, token_prefix, enabled, expires_at, created_at, created_by, label)
	  VALUES(?,?,?,?,?,?,?,?,?)`,
		t.ID, t.ClientID, []byte(t.TokenHash), t.Prefix, boolToInt(t.Enabled), t.ExpiresAt, t.CreatedAt, t.CreatedBy, t.Label)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("client: issue token: %w", err)
	}
	if err := raw.Commit(); err != nil {
		return IssuedToken{}, err
	}
	return IssuedToken{Token: t, Plaintext: plaintext}, nil
}

// LookupByPlaintext resolves an active token and records best-effort throttled
// usage telemetry. Telemetry failure never prevents subscription delivery.
func (s *TokenStore) LookupByPlaintext(plaintext string) (*SubscriptionToken, error) {
	if plaintext == "" {
		return nil, nil
	}
	now := nowUnix()
	row := s.db.QueryRow(`UPDATE subscription_tokens SET enabled=enabled
 WHERE token_hash=? AND revoked_at IS NULL AND enabled=1 AND (expires_at IS NULL OR expires_at>?)
 RETURNING id,client_id,token_hash,token_prefix,enabled,expires_at,created_at,last_used_at,revoked_at,created_by,label`, []byte(hashToken(plaintext)), now)
	token, err := scanToken(row, true)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("client: claim token: %w", err)
	}
	if telemetryErr := s.MarkUsedThrottled(token.ID); telemetryErr == nil {
		token.LastUsedAt = &now
	}
	return &token, nil
}

func (s *TokenStore) LookupReadOnly(plaintext string) (*SubscriptionToken, error) {
	if plaintext == "" {
		return nil, nil
	}
	h := hashToken(plaintext)
	now := nowUnix()
	row := s.db.QueryRow(`SELECT id,client_id,token_hash,token_prefix,enabled,expires_at,
 created_at,last_used_at,revoked_at,created_by,label FROM subscription_tokens
 WHERE token_hash=? AND revoked_at IS NULL AND enabled=1 AND (expires_at IS NULL OR expires_at>?)`, []byte(h), now)
	token, err := scanToken(row, true)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("client: lookup token: %w", err)
	}
	return &token, nil
}

func (s *TokenStore) MarkUsedThrottled(id string) error {
	now := nowUnix()
	s.usageMu.Lock()
	if previous := s.usage[id]; previous != 0 && now-previous < int64((5*time.Minute)/time.Second) {
		s.usageMu.Unlock()
		return nil
	}
	s.usage[id] = now
	s.usageMu.Unlock()
	_, err := s.db.Exec(`UPDATE subscription_tokens SET last_used_at=? WHERE id=?`, now, id)
	return err
}

func validateTokenMetadata(label string, expiresAt *int64) error {
	if !utf8.ValidString(label) || len(label) > 80 {
		return fmt.Errorf("%w: token label must be valid UTF-8 and at most 80 bytes", ErrValidation)
	}
	if expiresAt != nil && *expiresAt <= nowUnix() {
		return fmt.Errorf("%w: token expiry must be in the future", ErrValidation)
	}
	return nil
}

// Revoke invalidates a token by ID.
func (s *TokenStore) Revoke(id string) error {
	res, err := s.db.Exec(`UPDATE subscription_tokens SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, nowUnix(), id)
	if err != nil {
		return fmt.Errorf("client: revoke token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Get returns token metadata by ID.
func (s *TokenStore) Get(id string) (SubscriptionToken, error) {
	row := s.db.QueryRow(`SELECT id, client_id, token_hash, token_prefix, enabled, expires_at, created_at, last_used_at, revoked_at, created_by, label
  FROM subscription_tokens WHERE id=?`, id)
	token, err := scanToken(row, false)
	if err == sql.ErrNoRows {
		return SubscriptionToken{}, ErrNotFound
	}
	return token, err
}

// SetEnabled toggles a token without revoking it.
func (s *TokenStore) SetEnabled(id string, enabled bool) error {
	res, err := s.db.Exec(`UPDATE subscription_tokens SET enabled=? WHERE id=?`, boolToInt(enabled), id)
	if err != nil {
		return fmt.Errorf("client: toggle token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Rotate revokes the given token and issues a fresh one for the same client,
// returning the new plaintext once. Revoke + issue run in ONE transaction: the
// old implementation could revoke the token and then fail to issue the
// replacement, leaving the client without a usable subscription token.
func (s *TokenStore) Rotate(id string) (IssuedToken, error) {
	return s.RotateWithExpiry(id, nil, false)
}

func (s *TokenStore) RotateWithExpiry(id string, expiresAt *int64, expirySupplied bool) (IssuedToken, error) {
	raw, err := s.db.Begin()
	if err != nil {
		return IssuedToken{}, err
	}
	tx := &Tx{queries: queries{q: raw}, tx: raw}
	issued, err := tx.RotateTokenWithExpiryTx(id, expiresAt, expirySupplied)
	if err != nil {
		_ = raw.Rollback()
		return IssuedToken{}, err
	}
	if err := raw.Commit(); err != nil {
		return IssuedToken{}, err
	}
	return issued, nil
}

// ListForClient returns token metadata (no hash, no plaintext) for a client.
func (s *TokenStore) ListForClient(clientID string) ([]SubscriptionToken, error) {
	rows, err := s.db.Query(`SELECT id, client_id, token_hash, token_prefix, enabled, expires_at, created_at, last_used_at, revoked_at, created_by, label
	  FROM subscription_tokens WHERE client_id=? ORDER BY created_at DESC`, clientID)
	if err != nil {
		return nil, fmt.Errorf("client: list tokens: %w", err)
	}
	defer rows.Close()
	var out []SubscriptionToken
	for rows.Next() {
		t, err := scanToken(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// --- transactional variants used by the unified mutation orchestration ---
// These run inside a caller-managed Tx so token writes commit atomically with
// the desired-revision bump and the immutable snapshot.

// IssueTokenTx creates a token inside the transaction, returning plaintext once.
func (t *Tx) IssueTokenTx(clientID, label string, expiresAt *int64) (IssuedToken, error) {
	if err := validateTokenMetadata(label, expiresAt); err != nil {
		return IssuedToken{}, err
	}
	plaintext, err := generateToken()
	if err != nil {
		return IssuedToken{}, err
	}
	tok := SubscriptionToken{
		ID:        uuid.NewString(),
		ClientID:  clientID,
		TokenHash: hashToken(plaintext),
		Prefix:    tokenPrefix(plaintext),
		Label:     label,
		Enabled:   true,
		ExpiresAt: expiresAt,
		CreatedAt: nowUnix(),
	}
	_, err = t.q.Exec(`INSERT INTO subscription_tokens
	  (id, client_id, token_hash, token_prefix, enabled, expires_at, created_at, created_by, label)
	  VALUES(?,?,?,?,?,?,?,?,?)`,
		tok.ID, tok.ClientID, []byte(tok.TokenHash), tok.Prefix, boolToInt(tok.Enabled), tok.ExpiresAt, tok.CreatedAt, "", tok.Label)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("client: issue token: %w", err)
	}
	return IssuedToken{Token: tok, Plaintext: plaintext}, nil
}

// RevokeTokenTx invalidates a token by ID inside the transaction.
func (t *Tx) RevokeTokenTx(id string) error {
	res, err := t.q.Exec(`UPDATE subscription_tokens SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, nowUnix(), id)
	if err != nil {
		return fmt.Errorf("client: revoke token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTokenEnabledTx toggles a token inside the transaction.
func (t *Tx) SetTokenEnabledTx(id string, enabled bool) error {
	res, err := t.q.Exec(`UPDATE subscription_tokens SET enabled=? WHERE id=?`, boolToInt(enabled), id)
	if err != nil {
		return fmt.Errorf("client: toggle token: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RotateTokenTx revokes the given token and issues a fresh one for the same
// client — revoke + issue inside the transaction so a token can never be
// revoked without its replacement being stored.
func (t *Tx) RotateTokenTx(id string) (IssuedToken, error) {
	return t.RotateTokenWithExpiryTx(id, nil, false)
}

func (t *Tx) RotateTokenWithExpiryTx(id string, expiresAt *int64, expirySupplied bool) (IssuedToken, error) {
	row := t.q.QueryRow(`SELECT id, client_id, token_hash, token_prefix, enabled, expires_at, created_at, last_used_at, revoked_at, created_by, label
  FROM subscription_tokens WHERE id=?`, id)
	tok, err := scanToken(row, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return IssuedToken{}, ErrNotFound
		}
		return IssuedToken{}, err
	}
	if tok.RevokedAt != nil {
		return IssuedToken{}, fmt.Errorf("%w: token already revoked", ErrValidation)
	}
	if tok.ExpiresAt != nil && nowUnix() >= *tok.ExpiresAt && !expirySupplied {
		return IssuedToken{}, fmt.Errorf("%w: rotating an expired token requires a new expiry", ErrValidation)
	}
	if expirySupplied {
		tok.ExpiresAt = expiresAt
	}
	if err := t.RevokeTokenTx(id); err != nil {
		return IssuedToken{}, err
	}
	return t.IssueTokenTx(tok.ClientID, tok.Label, tok.ExpiresAt)
}

func scanToken(row scanner, withHash bool) (SubscriptionToken, error) {
	var t SubscriptionToken
	var hashBytes []byte
	var enabled int
	err := row.Scan(&t.ID, &t.ClientID, &hashBytes, &t.Prefix, &enabled,
		&t.ExpiresAt, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt, &t.CreatedBy, &t.Label)
	if err != nil {
		return SubscriptionToken{}, err
	}
	if withHash {
		t.TokenHash = string(hashBytes)
	}
	t.Enabled = enabled == 1
	return t, nil
}

func generateToken() (string, error) {
	b := make([]byte, tokenByteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("client: token rand: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return string(sum[:])
}

func tokenPrefix(plaintext string) string {
	if len(plaintext) <= tokenPrefixLen {
		return plaintext
	}
	return plaintext[:tokenPrefixLen]
}
