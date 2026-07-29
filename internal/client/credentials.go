package client

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

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

// RevealEncrypted decrypts immutable credential bytes without consulting the
// mutable credential table.
func (s *CredentialStore) RevealEncrypted(encrypted []byte) (string, error) {
	if s == nil || s.cipher == nil {
		return "", fmt.Errorf("client: credential cipher unavailable")
	}
	plaintext, err := s.cipher.Decrypt(string(encrypted))
	if err != nil {
		return "", fmt.Errorf("client: decrypt credential snapshot: %w", err)
	}
	return plaintext, nil
}

// Set encrypts and stores a credential for a binding, creating credential
// version 1 (or the next version if one already exists for that kind).
func (s *CredentialStore) Set(bindingID, kind, plaintext string) (Credential, error) {
	if s.cipher == nil {
		return Credential{}, fmt.Errorf("client: credential cipher unavailable")
	}
	return insertCredentialQ(s.db, s.cipher, bindingID, kind, plaintext, nextVersionForQ(s.db, bindingID, kind))
}

// insertCredentialQ is the querier-based insert shared by the autocommit store
// and the transactional Tx path.
func insertCredentialQ(q DBTX, cipher *secrets.Cipher, bindingID, kind, plaintext string, version int) (Credential, error) {
	enc, err := cipher.Encrypt(plaintext)
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
	_, err = q.Exec(`INSERT INTO client_credentials
  (id, binding_id, kind, encrypted_value, key_version, credential_version, created_at)
  VALUES(?,?,?,?,?,?,?)`,
		c.ID, c.BindingID, c.Kind, c.EncryptedValue, c.KeyVersion, c.CredentialVersion, c.CreatedAt)
	if err != nil {
		return Credential{}, fmt.Errorf("client: store credential: %w", err)
	}
	return c, nil
}

// SetCredential encrypts and stores a credential within the transaction. The
// cipher is passed explicitly so the Tx does not need a reference to the
// CredentialStore's DB handle (which would bypass the transaction).
func (t *Tx) SetCredential(creds *CredentialStore, bindingID, kind, plaintext string) (Credential, error) {
	if creds.cipher == nil {
		return Credential{}, fmt.Errorf("client: credential cipher unavailable")
	}
	return insertCredentialQ(t.q, creds.cipher, bindingID, kind, plaintext, nextVersionForQ(t.q, bindingID, kind))
}

// RotateCredential revokes the active credential of the given kind and stores
// a new version — revoke, insert, and rotated_at all inside the transaction,
// so a failure can never leave a binding without an active credential while
// the old one is already revoked.
func (t *Tx) RotateCredential(creds *CredentialStore, bindingID, kind, plaintext string) (Credential, error) {
	if creds.cipher == nil {
		return Credential{}, fmt.Errorf("client: credential cipher unavailable")
	}
	now := nowUnix()
	if _, err := t.q.Exec(`UPDATE client_credentials SET revoked_at=? WHERE binding_id=? AND kind=? AND revoked_at IS NULL`, now, bindingID, kind); err != nil {
		return Credential{}, fmt.Errorf("client: revoke old credential: %w", err)
	}
	newCred, err := insertCredentialQ(t.q, creds.cipher, bindingID, kind, plaintext, nextVersionForQ(t.q, bindingID, kind))
	if err != nil {
		return Credential{}, err
	}
	newCred.RotatedAt = &now
	if _, err := t.q.Exec(`UPDATE client_credentials SET rotated_at=? WHERE id=?`, now, newCred.ID); err != nil {
		return Credential{}, fmt.Errorf("client: mark rotated credential: %w", err)
	}
	return newCred, nil
}

// ReencryptCredentials moves every normalized credential to a replacement
// master cipher within the caller's transaction. Active and revoked rows are
// both covered so no live database ciphertext is stranded under the old key.
func (t *Tx) ReencryptCredentials(oldCipher, newCipher *secrets.Cipher) error {
	if oldCipher == nil || newCipher == nil {
		return fmt.Errorf("client: credential rotation ciphers unavailable")
	}
	rows, err := t.q.Query(`SELECT id, encrypted_value FROM client_credentials ORDER BY id`)
	if err != nil {
		return fmt.Errorf("client: list credentials for master-key rotation: %w", err)
	}
	type encryptedCredential struct {
		id    string
		value []byte
	}
	var credentials []encryptedCredential
	for rows.Next() {
		var credential encryptedCredential
		if err := rows.Scan(&credential.id, &credential.value); err != nil {
			_ = rows.Close()
			return fmt.Errorf("client: scan credential for master-key rotation: %w", err)
		}
		credentials = append(credentials, credential)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("client: close credential rotation rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("client: list credentials for master-key rotation: %w", err)
	}
	for _, credential := range credentials {
		plaintext, err := oldCipher.Decrypt(string(credential.value))
		if err != nil {
			return fmt.Errorf("client: decrypt credential %s for master-key rotation: %w", credential.id, err)
		}
		encrypted, err := newCipher.Encrypt(plaintext)
		if err != nil {
			return fmt.Errorf("client: encrypt credential %s for master-key rotation: %w", credential.id, err)
		}
		if _, err := t.q.Exec(`UPDATE client_credentials SET encrypted_value=?, key_version=key_version+1 WHERE id=?`, []byte(encrypted), credential.id); err != nil {
			return fmt.Errorf("client: persist credential %s master-key rotation: %w", credential.id, err)
		}
	}
	return nil
}

// Rotate revokes the current active credential of the given kind and stores a
// new version with fresh plaintext, returning the new active credential. The
// revoke + insert + rotated_at write run in ONE transaction — the previous
// implementation committed the revoke first and inserted the replacement in
// autocommit, which could strand a binding with no active credential.
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
	newCred, err := insertCredentialQ(tx, s.cipher, bindingID, kind, plaintext, nextVersionForQ(tx, bindingID, kind))
	if err != nil {
		return Credential{}, err
	}
	newCred.RotatedAt = &now
	if _, err := tx.Exec(`UPDATE client_credentials SET rotated_at=? WHERE id=?`, now, newCred.ID); err != nil {
		return Credential{}, fmt.Errorf("client: mark rotated credential: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Credential{}, err
	}
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
	return activeCredentialQ(s.db, bindingID, kind)
}

// activeCredentialQ is the querier-based ActiveForBinding shared by the
// autocommit store and the transactional Tx path.
func activeCredentialQ(q interface {
	QueryRow(query string, args ...any) *sql.Row
}, bindingID, kind string) (Credential, error) {
	row := q.QueryRow(`SELECT id, binding_id, kind, encrypted_value, key_version, credential_version,
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
	value := string(cred.EncryptedValue)
	if !strings.HasPrefix(value, "ve1:") {
		return "", errors.New("client: normalized credential ciphertext is invalid")
	}
	return s.cipher.Decrypt(value)
}

// ListForBinding returns credential metadata for a binding WITHOUT the
// encrypted material, so list endpoints never carry secrets.
func (s *CredentialStore) ListForBinding(bindingID string) ([]Credential, error) {
	return listCredentialsForBindingQ(s.db, bindingID)
}

// listCredentialsForBindingQ is the querier-based ListForBinding shared by the
// autocommit store and the transactional Tx path.
func listCredentialsForBindingQ(q interface {
	Query(query string, args ...any) (*sql.Rows, error)
}, bindingID string) ([]Credential, error) {
	rows, err := q.Query(`SELECT id, binding_id, kind, encrypted_value, key_version, credential_version,
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

// nextVersionForQ computes the next credential version on any querier. When
// called with a *sql.Tx it sees uncommitted rows in the same transaction.
func nextVersionForQ(q interface {
	QueryRow(query string, args ...any) *sql.Row
}, bindingID, kind string) int {
	var max int
	_ = q.QueryRow(`SELECT COALESCE(MAX(credential_version),0) FROM client_credentials WHERE binding_id=? AND kind=?`, bindingID, kind).Scan(&max)
	return max + 1
}
