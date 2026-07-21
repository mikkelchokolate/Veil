package client

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"

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
type TokenStore struct{ db *sql.DB }

func NewTokenStore(db *sql.DB) *TokenStore { return &TokenStore{db: db} }

// Issue creates a new token for a client, returning the plaintext once.
// Optional expiry (unix seconds) bounds how long the token grants access.
func (s *TokenStore) Issue(clientID, label string, expiresAt *int64) (IssuedToken, error) {
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
		Enabled:   true,
		ExpiresAt: expiresAt,
		CreatedAt: nowUnix(),
	}
	_, err = s.db.Exec(`INSERT INTO subscription_tokens
	  (id, client_id, token_hash, token_prefix, enabled, expires_at, created_at, created_by)
	  VALUES(?,?,?,?,?,?,?,?)`,
		t.ID, t.ClientID, []byte(t.TokenHash), t.Prefix, boolToInt(t.Enabled), t.ExpiresAt, t.CreatedAt, t.Label)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("client: issue token: %w", err)
	}
	return IssuedToken{Token: t, Plaintext: plaintext}, nil
}

// LookupByPlaintext resolves an active (enabled, unrevoked, unexpired) token
// from its plaintext, updating last_used_at. Returns (nil, nil) when unknown,
// disabled, revoked, or expired.
func (s *TokenStore) LookupByPlaintext(plaintext string) (*SubscriptionToken, error) {
	if plaintext == "" {
		return nil, nil
	}
	h := hashToken(plaintext)
	row := s.db.QueryRow(`SELECT id, client_id, token_hash, token_prefix, enabled, expires_at, created_at, last_used_at, revoked_at, created_by
	  FROM subscription_tokens WHERE token_hash=? AND revoked_at IS NULL AND enabled=1`, []byte(h))
	t, err := scanToken(row, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if t.ExpiresAt != nil && nowUnix() >= *t.ExpiresAt {
		return nil, nil // expired
	}
	now := nowUnix()
	_, _ = s.db.Exec(`UPDATE subscription_tokens SET last_used_at=? WHERE id=?`, now, t.ID)
	t.LastUsedAt = &now
	return &t, nil
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
	raw, err := s.db.Begin()
	if err != nil {
		return IssuedToken{}, err
	}
	tx := &Tx{queries: queries{q: raw}, tx: raw}
	issued, err := tx.RotateTokenTx(id)
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
	rows, err := s.db.Query(`SELECT id, client_id, token_hash, token_prefix, enabled, expires_at, created_at, last_used_at, revoked_at, created_by
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
  (id, client_id, token_hash, token_prefix, enabled, expires_at, created_at, created_by)
  VALUES(?,?,?,?,?,?,?,?)`,
		tok.ID, tok.ClientID, []byte(tok.TokenHash), tok.Prefix, boolToInt(tok.Enabled), tok.ExpiresAt, tok.CreatedAt, tok.Label)
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
	row := t.q.QueryRow(`SELECT id, client_id, token_hash, token_prefix, enabled, expires_at, created_at, last_used_at, revoked_at, created_by
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
		&t.ExpiresAt, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt, &t.Label)
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
