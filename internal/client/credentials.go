package client

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

// CredentialStore manages encrypted per-binding credential material. Plaintext
// is encrypted with the panel's master secrets.Cipher before it touches the
// database, never logged, and never returned by list endpoints.
type CredentialStore struct {
	db     *sql.DB
	cipher *secrets.Cipher
}

func NewCredentialStore(db *sql.DB, cipher *secrets.Cipher) *CredentialStore {
	return &CredentialStore{db: db, cipher: cipher}
}

// Set encrypts and stores a credential for a binding, creating credential
// version 1 (or the next version if one already exists for that kind).
func (s *CredentialStore) Set(bindingID, kind, plaintext string) (Credential, error) {
	return s.insert(bindingID, kind, plaintext, nextVersionFor(s.db, bindingID, kind))
}

func (s *CredentialStore) insert(bindingID, kind, plaintext string, version int) (Credential, error) {
	if s.cipher == nil {
		return Credential{}, fmt.Errorf("client: credential cipher unavailable")
	}
	enc, err := s.cipher.Encrypt(plaintext)
	if err != nil {
		return Credential{}, fmt.Errorf("client: encrypt credential: %w", err)
	}
	c := Credential{
		ID:                uuid.NewString(),
		BindingID:         bindingID,
		Kind:              kind,
		EncryptedValue:    []byte(enc),
		KeyVersion:        1,
		CredentialVersion: version,
		CreatedAt:         nowUnix(),
	}
	_, err = s.db.Exec(`INSERT INTO client_credentials
	  (id, binding_id, kind, encrypted_value, key_version, credential_version, created_at)
	  VALUES(?,?,?,?,?,?,?)`,
		c.ID, c.BindingID, c.Kind, c.EncryptedValue, c.KeyVersion, c.CredentialVersion, c.CreatedAt)
	if err != nil {
		return Credential{}, fmt.Errorf("client: store credential: %w", err)
	}
	return c, nil
}

// Rotate revokes the current active credential of the given kind and stores a
// new version with fresh plaintext, returning the new active credential.
func (s *CredentialStore) Rotate(bindingID, kind, plaintext string) (Credential, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Credential{}, err
	}
	defer tx.Rollback()
	now := nowUnix()
	if _, err := tx.Exec(`UPDATE client_credentials SET revoked_at=? WHERE binding_id=? AND kind=? AND revoked_at IS NULL`, now, bindingID, kind); err != nil {
		return Credential{}, fmt.Errorf("client: revoke old credential: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Credential{}, err
	}
	newCred, err := s.insert(bindingID, kind, plaintext, nextVersionFor(s.db, bindingID, kind))
	if err != nil {
		return Credential{}, err
	}
	rotatedAt := now
	newCred.RotatedAt = &rotatedAt
	_, _ = s.db.Exec(`UPDATE client_credentials SET rotated_at=? WHERE id=?`, now, newCred.ID)
	return newCred, nil
}

// Get returns a credential by ID (without decrypting).
func (s *CredentialStore) Get(id string) (Credential, error) {
	row := s.db.QueryRow(`SELECT id, binding_id, kind, encrypted_value, key_version, credential_version,
	  created_at, rotated_at, revoked_at FROM client_credentials WHERE id=?`, id)
	return scanCredential(row, true)
}

// ActiveForBinding returns the current (unrevoked) credential of a kind.
func (s *CredentialStore) ActiveForBinding(bindingID, kind string) (Credential, error) {
	row := s.db.QueryRow(`SELECT id, binding_id, kind, encrypted_value, key_version, credential_version,
	  created_at, rotated_at, revoked_at FROM client_credentials
	  WHERE binding_id=? AND kind=? AND revoked_at IS NULL
	  ORDER BY credential_version DESC LIMIT 1`, bindingID, kind)
	return scanCredential(row, true)
}

// Reveal decrypts a credential's plaintext. Use sparingly — only where the
// value is genuinely required (rendering, subscription artifact).
func (s *CredentialStore) Reveal(id string) (string, error) {
	cred, err := s.Get(id)
	if err != nil {
		return "", err
	}
	if s.cipher == nil {
		return "", fmt.Errorf("client: credential cipher unavailable")
	}
	return s.cipher.Decrypt(string(cred.EncryptedValue))
}

// ListForBinding returns credential metadata for a binding WITHOUT the
// encrypted material, so list endpoints never carry secrets.
func (s *CredentialStore) ListForBinding(bindingID string) ([]Credential, error) {
	rows, err := s.db.Query(`SELECT id, binding_id, kind, encrypted_value, key_version, credential_version,
	  created_at, rotated_at, revoked_at FROM client_credentials
	  WHERE binding_id=? ORDER BY kind, credential_version DESC`, bindingID)
	if err != nil {
		return nil, fmt.Errorf("client: list credentials: %w", err)
	}
	defer rows.Close()
	var out []Credential
	for rows.Next() {
		c, err := scanCredential(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanCredential(row scanner, withValue bool) (Credential, error) {
	var c Credential
	var enc []byte
	err := row.Scan(&c.ID, &c.BindingID, &c.Kind, &enc, &c.KeyVersion, &c.CredentialVersion,
		&c.CreatedAt, &c.RotatedAt, &c.RevokedAt)
	if err != nil {
		return Credential{}, err
	}
	if withValue {
		c.EncryptedValue = enc
	}
	return c, nil
}

func nextVersionFor(db *sql.DB, bindingID, kind string) int {
	var max int
	_ = db.QueryRow(`SELECT COALESCE(MAX(credential_version),0) FROM client_credentials WHERE binding_id=? AND kind=?`, bindingID, kind).Scan(&max)
	return max + 1
}

// nextVersionForTx is the transactional variant used within a WithTx block so
// the version computation sees uncommitted rows in the same transaction.
func nextVersionForTx(tx *sql.Tx, bindingID, kind string) int {
	var max int
	_ = tx.QueryRow(`SELECT COALESCE(MAX(credential_version),0) FROM client_credentials WHERE binding_id=? AND kind=?`, bindingID, kind).Scan(&max)
	return max + 1
}
