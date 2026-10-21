package client

import (
	"container/list"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

var ErrSecretUnavailable = errors.New("client: token secret is not recoverable")

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
type tokenUsageEvent struct {
	id string
	at int64
}

type TokenStore struct {
	db            *sql.DB
	cipher        *secrets.Cipher
	binding       *secrets.Cipher
	legacyMu      sync.Mutex
	legacySealed  bool
	usageMu       sync.Mutex
	usage         map[string]int64
	usageOrder    *list.List
	usageElements map[string]*list.Element
	telemetry     chan tokenUsageEvent
	stop          chan struct{}
	done          chan struct{}
	closeOnce     sync.Once
}

func NewTokenStore(db *sql.DB) *TokenStore {
	store := &TokenStore{
		db: db, usage: make(map[string]int64), usageOrder: list.New(), usageElements: make(map[string]*list.Element),
		telemetry: make(chan tokenUsageEvent, 1024), stop: make(chan struct{}), done: make(chan struct{}),
	}
	go store.runTelemetry()
	return store
}

func (s *TokenStore) WithCipher(cipher *secrets.Cipher) *TokenStore {
	if s != nil {
		s.cipher = cipher
		s.binding = nil
		s.legacySealed = false
		if cipher != nil {
			// A derive failure leaves binding nil, which fails closed: no
			// token can be issued, sealed or authenticated.
			s.binding, _ = TokenBindingCipher(cipher)
		}
	}
	return s
}

// TokenBindingCipher derives the subkey that seals subscription token
// issuance bindings. Token seals use their own subkey so no other ciphertext
// sealed under the state key (client credentials, snapshot secrets) can
// ever open as a token binding (#1281).
func TokenBindingCipher(master *secrets.Cipher) (*secrets.Cipher, error) {
	return secrets.DeriveCipher(master, tokenBindingVersion)
}

// Issue creates a new token for a client, returning the plaintext once.
// Optional expiry (unix seconds) bounds how long the token grants access.
func (s *TokenStore) Issue(clientID, label string, expiresAt *int64) (IssuedToken, error) {
	return s.IssueBy(clientID, label, "", expiresAt)
}

func (s *TokenStore) IssueBy(clientID, label, createdBy string, expiresAt *int64) (IssuedToken, error) {
	if err := validateTokenMetadata(label, expiresAt); err != nil {
		return IssuedToken{}, err
	}
	// Every row must carry a sealed issuance binding, otherwise lookups
	// refuse it (#1281); issuing a token that can never authenticate would
	// only hand the operator a dead link.
	if s == nil || s.binding == nil {
		return IssuedToken{}, ErrSecretUnavailable
	}
	if err := s.ensureLegacyBindingsSealed(); err != nil {
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
	ciphertext, err := s.sealToken(t, plaintext)
	if err != nil {
		return IssuedToken{}, err
	}
	t.HasSecret = true
	_, err = raw.Exec(`INSERT INTO subscription_tokens
	  (id, client_id, token_hash, token_prefix, enabled, expires_at, created_at, created_by, label, token_ciphertext)
	  VALUES(?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.ClientID, []byte(t.TokenHash), t.Prefix, boolToInt(t.Enabled), t.ExpiresAt, t.CreatedAt, t.CreatedBy, t.Label, ciphertext)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("client: issue token: %w", err)
	}
	if err := raw.Commit(); err != nil {
		return IssuedToken{}, err
	}
	return IssuedToken{Token: t, Plaintext: plaintext}, nil
}

// LookupByPlaintext resolves an active token using a read-only query and queues
// bounded, coalesced usage telemetry.
func (s *TokenStore) LookupByPlaintext(plaintext string) (*SubscriptionToken, error) {
	token, err := s.LookupReadOnly(plaintext)
	if err != nil || token == nil {
		return token, err
	}
	now := nowUnix()
	if telemetryErr := s.MarkUsedThrottled(token.ID); telemetryErr == nil {
		token.LastUsedAt = &now
	}
	return token, nil
}

// LookupReadOnly resolves an active token. A row only authenticates when its
// token_ciphertext is a panel-sealed issuance binding (see sealToken) that
// matches the row's id, client, expiry and token hash. A veil.db writer
// without the state key can therefore neither plant a row for a chosen
// token, nor re-point, re-hash or extend an issued one (#1281). Rows that
// fail verification are an indistinguishable miss, like unknown tokens.
func (s *TokenStore) LookupReadOnly(plaintext string) (*SubscriptionToken, error) {
	if plaintext == "" || s == nil || s.binding == nil {
		return nil, nil
	}
	h := hashToken(plaintext)
	now := nowUnix()
	row := s.db.QueryRow(`SELECT id,client_id,token_hash,token_prefix,enabled,expires_at,
 created_at,last_used_at,revoked_at,created_by,label,token_ciphertext FROM subscription_tokens
 WHERE token_hash=? AND revoked_at IS NULL AND enabled=1 AND (expires_at IS NULL OR expires_at>?)`, []byte(h), now)
	token, ciphertext, err := scanTokenWithCiphertext(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("client: lookup token: %w", err)
	}
	opened, ok := s.openToken(token, ciphertext)
	if !ok || subtle.ConstantTimeCompare([]byte(opened), []byte(plaintext)) != 1 {
		return nil, nil
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
	s.usageMu.Unlock()
	select {
	case s.telemetry <- tokenUsageEvent{id: id, at: now}:
		s.usageMu.Lock()
		s.usage[id] = now
		if element := s.usageElements[id]; element != nil {
			s.usageOrder.MoveToFront(element)
		} else {
			s.usageElements[id] = s.usageOrder.PushFront(id)
		}
		for s.usageOrder.Len() > 4096 {
			oldest := s.usageOrder.Back()
			oldID := oldest.Value.(string)
			delete(s.usage, oldID)
			delete(s.usageElements, oldID)
			s.usageOrder.Remove(oldest)
		}
		s.usageMu.Unlock()
		return nil
	default:
		return errors.New("client: token telemetry queue is full")
	}
}

func (s *TokenStore) forgetUsage(id string) {
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	delete(s.usage, id)
	if element := s.usageElements[id]; element != nil {
		delete(s.usageElements, id)
		s.usageOrder.Remove(element)
	}
}

func (s *TokenStore) flushTelemetry(pending map[string]int64) {
	if len(pending) == 0 {
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		for id := range pending {
			s.forgetUsage(id)
		}
		return
	}
	defer tx.Rollback()
	for id, at := range pending {
		result, updateErr := tx.Exec(`UPDATE subscription_tokens SET last_used_at=? WHERE id=? AND revoked_at IS NULL AND enabled=1 AND (expires_at IS NULL OR expires_at>?)`, at, id, at)
		if updateErr != nil {
			err = updateErr
			break
		}
		if rows, _ := result.RowsAffected(); rows != 1 {
			s.forgetUsage(id)
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		for id := range pending {
			s.forgetUsage(id)
		}
	}
}

func (s *TokenStore) runTelemetry() {
	defer close(s.done)
	pending := make(map[string]int64)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case event := <-s.telemetry:
			pending[event.id] = event.at
		case <-ticker.C:
			s.flushTelemetry(pending)
			pending = make(map[string]int64)
		case <-s.stop:
			s.flushTelemetry(pending)
			return
		}
	}
}

func (s *TokenStore) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		close(s.stop)
		<-s.done
	})
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
	s.forgetUsage(id)
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
	if !enabled {
		s.forgetUsage(id)
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
	if s == nil || s.binding == nil {
		return IssuedToken{}, ErrSecretUnavailable
	}
	if err := s.ensureLegacyBindingsSealed(); err != nil {
		return IssuedToken{}, err
	}
	raw, err := s.db.Begin()
	if err != nil {
		return IssuedToken{}, err
	}
	tx := &Tx{queries: queries{q: raw}, tx: raw}
	issued, err := tx.rotateTokenWithExpiryTx(id, expiresAt, expirySupplied)
	if err != nil {
		_ = raw.Rollback()
		return IssuedToken{}, err
	}
	ciphertext, err := s.sealToken(issued.Token, issued.Plaintext)
	if err != nil {
		_ = raw.Rollback()
		return IssuedToken{}, err
	}
	if _, err := raw.Exec(`UPDATE subscription_tokens SET token_ciphertext=? WHERE id=?`, ciphertext, issued.Token.ID); err != nil {
		_ = raw.Rollback()
		return IssuedToken{}, fmt.Errorf("client: persist rotated token: %w", err)
	}
	issued.Token.HasSecret = true
	if err := raw.Commit(); err != nil {
		return IssuedToken{}, err
	}
	s.forgetUsage(id)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.markRecoverableSecrets(clientID, out); err != nil {
		return nil, err
	}
	return out, nil
}

// markRecoverableSecrets sets HasSecret on each token whose stored
// ciphertext is a valid issuance binding for its row, i.e. exactly the tokens
// Reveal can rebuild a URL for. A mid-iteration failure must fail the listing
// rather than silently downgrading flags — otherwise recoverable tokens look
// unrecoverable (#1063).
func (s *TokenStore) markRecoverableSecrets(clientID string, tokens []SubscriptionToken) error {
	if len(tokens) == 0 {
		return nil
	}
	rows, err := s.db.Query(`SELECT id,client_id,token_hash,token_prefix,enabled,expires_at,
 created_at,last_used_at,revoked_at,created_by,label,token_ciphertext FROM subscription_tokens
 WHERE client_id=? AND token_ciphertext IS NOT NULL AND token_ciphertext<>''`, clientID)
	if err != nil {
		return fmt.Errorf("client: list recoverable token secrets: %w", err)
	}
	defer rows.Close()
	have := map[string]struct{}{}
	for rows.Next() {
		token, ciphertext, err := scanTokenWithCiphertext(rows)
		if err != nil {
			return fmt.Errorf("client: scan recoverable token secret: %w", err)
		}
		if _, ok := s.openToken(token, ciphertext); ok {
			have[token.ID] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("client: iterate recoverable token secrets: %w", err)
	}
	for i := range tokens {
		_, tokens[i].HasSecret = have[tokens[i].ID]
	}
	return nil
}

// tokenBindingVersion tags the sealed issuance binding stored in
// token_ciphertext and labels the subkey it is sealed with. Rows issued
// before #1281 sealed the bare token plaintext under the master cipher;
// SealLegacyBindings re-seals them once.
const tokenBindingVersion = "veil-subscription-token-binding-v1"

// TokenBindingMarkerKey records in migration_markers that the one-shot
// legacy re-seal ran, so a later bare master-cipher seal is never trusted.
const TokenBindingMarkerKey = "subscription_token_binding_v1"

// tokenBinding is the AEAD-sealed record that ties a token to the row the
// panel issued it on. Every field that decides who the token authenticates as
// and for how long is inside the seal; revoked_at/enabled only ever narrow
// access and stay plain columns.
type tokenBinding struct {
	Version   string `json:"v"`
	ID        string `json:"id"`
	ClientID  string `json:"client_id"`
	ExpiresAt *int64 `json:"expires_at"`
	Token     string `json:"token"`
}

func (s *TokenStore) sealToken(t SubscriptionToken, plaintext string) (string, error) {
	if s == nil {
		return "", ErrSecretUnavailable
	}
	return sealTokenBinding(s.binding, t, plaintext)
}

func sealTokenBinding(binding *secrets.Cipher, t SubscriptionToken, plaintext string) (string, error) {
	if binding == nil {
		return "", ErrSecretUnavailable
	}
	if plaintext == "" {
		return "", fmt.Errorf("client: seal token: empty plaintext")
	}
	payload, err := json.Marshal(tokenBinding{
		Version: tokenBindingVersion, ID: t.ID, ClientID: t.ClientID, ExpiresAt: t.ExpiresAt, Token: plaintext,
	})
	if err != nil {
		return "", fmt.Errorf("client: seal token: %w", err)
	}
	value, err := binding.Encrypt(string(payload))
	if err != nil {
		return "", fmt.Errorf("client: encrypt token: %w", err)
	}
	return value, nil
}

// openToken verifies the row's token_ciphertext against the row and returns
// the token plaintext it authorizes. Only a binding sealed under the token
// subkey is accepted: an empty or non-"ve1:" value (Cipher.Decrypt would pass
// it through, #1283), any other ciphertext sealed under the state key (for
// example a client's own credential, #1281 review), a binding for another
// id/client/expiry, and a token that does not hash to the row's token_hash
// are all refused.
func (s *TokenStore) openToken(row SubscriptionToken, ciphertext string) (string, bool) {
	if s == nil {
		return "", false
	}
	return openTokenBinding(s.binding, row, ciphertext)
}

func openTokenBinding(bindingCipher *secrets.Cipher, row SubscriptionToken, ciphertext string) (string, bool) {
	if bindingCipher == nil || !secrets.IsEncrypted(ciphertext) {
		return "", false
	}
	opened, err := bindingCipher.Decrypt(ciphertext)
	if err != nil {
		return "", false
	}
	var binding tokenBinding
	if err := json.Unmarshal([]byte(opened), &binding); err != nil {
		return "", false
	}
	if binding.Version != tokenBindingVersion || binding.ID != row.ID || binding.ClientID != row.ClientID ||
		!sameExpiry(binding.ExpiresAt, row.ExpiresAt) || binding.Token == "" {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(hashToken(binding.Token)), []byte(row.TokenHash)) != 1 {
		return "", false
	}
	return binding.Token, true
}

// ensureLegacyBindingsSealed runs the one-shot legacy re-seal before the
// store writes its first binding. SealLegacyBindings treats an existing
// binding without a marker as tampering, so issuing ahead of a failed
// startup pass would otherwise strand every pre-#1281 token.
func (s *TokenStore) ensureLegacyBindingsSealed() error {
	s.legacyMu.Lock()
	defer s.legacyMu.Unlock()
	if s.legacySealed {
		return nil
	}
	if _, err := s.sealLegacyBindings(); err != nil {
		return fmt.Errorf("client: seal legacy token bindings: %w", err)
	}
	s.legacySealed = true
	return nil
}

// SealLegacyBindings re-seals, once per database, the token rows issued
// before #1281: their token_ciphertext is the bare token sealed under the
// master cipher, which carries no client/expiry binding and cannot be told
// apart from any other master-cipher seal. Each row whose master-cipher
// plaintext hashes to its token_hash becomes a binding of its current
// (id, client, expiry). The run is recorded in migration_markers; after that
// only subkey-sealed bindings authenticate. A database that already holds a
// subkey-sealed binding has been through this pass, so a missing marker there
// means it was deleted: the marker is restored and nothing is re-sealed.
func (s *TokenStore) SealLegacyBindings() (int, error) {
	if s == nil {
		return 0, ErrSecretUnavailable
	}
	s.legacyMu.Lock()
	defer s.legacyMu.Unlock()
	resealed, err := s.sealLegacyBindings()
	if err == nil {
		s.legacySealed = true
	}
	return resealed, err
}

func (s *TokenStore) sealLegacyBindings() (int, error) {
	if s.cipher == nil || s.binding == nil {
		return 0, ErrSecretUnavailable
	}
	raw, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer raw.Rollback() //nolint:errcheck
	q := queries{q: raw}
	marker, err := q.GetMigrationMarker(TokenBindingMarkerKey)
	if err != nil {
		return 0, err
	}
	if marker != nil {
		return 0, nil
	}
	rows, err := raw.Query(`SELECT id,client_id,token_hash,token_prefix,enabled,expires_at,
 created_at,last_used_at,revoked_at,created_by,label,token_ciphertext FROM subscription_tokens
 WHERE token_ciphertext IS NOT NULL AND token_ciphertext<>'' ORDER BY id`)
	if err != nil {
		return 0, fmt.Errorf("client: list tokens for binding re-seal: %w", err)
	}
	type legacyRow struct {
		token     SubscriptionToken
		plaintext string
	}
	var legacy []legacyRow
	bound := 0
	for rows.Next() {
		token, ciphertext, err := scanTokenWithCiphertext(rows)
		if err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("client: scan token for binding re-seal: %w", err)
		}
		if _, ok := s.openToken(token, ciphertext); ok {
			bound++
			continue
		}
		if !secrets.IsEncrypted(ciphertext) {
			continue
		}
		plaintext, err := s.cipher.Decrypt(ciphertext)
		if err != nil || plaintext == "" ||
			subtle.ConstantTimeCompare([]byte(hashToken(plaintext)), []byte(token.TokenHash)) != 1 {
			continue
		}
		legacy = append(legacy, legacyRow{token: token, plaintext: plaintext})
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("client: close binding re-seal rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("client: list tokens for binding re-seal: %w", err)
	}
	if bound > 0 {
		legacy = nil
	}
	for _, row := range legacy {
		sealed, err := s.sealToken(row.token, row.plaintext)
		if err != nil {
			return 0, err
		}
		if _, err := raw.Exec(`UPDATE subscription_tokens SET token_ciphertext=? WHERE id=?`, sealed, row.token.ID); err != nil {
			return 0, fmt.Errorf("client: persist token %s binding re-seal: %w", row.token.ID, err)
		}
	}
	details, err := json.Marshal(map[string]int{"resealed": len(legacy), "already_bound": bound})
	if err != nil {
		return 0, err
	}
	if err := q.PutMigrationMarker(MigrationMarker{Key: TokenBindingMarkerKey, Version: 1, AppliedAt: nowUnix(), Details: string(details)}); err != nil {
		return 0, err
	}
	if err := raw.Commit(); err != nil {
		return 0, err
	}
	return len(legacy), nil
}

func sameExpiry(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// Reveal decrypts the stored subscription token so the panel can rebuild the
// URL and QR after the original issue response is gone.
func (s *TokenStore) Reveal(id string) (string, error) {
	if s == nil || s.binding == nil {
		return "", ErrSecretUnavailable
	}
	// Reveal mirrors SubscriptionToken.IsActive: revoked, disabled, and
	// expired tokens no longer authenticate, so their secret must not be
	// recoverable through the store either (#966).
	row := s.db.QueryRow(`SELECT id,client_id,token_hash,token_prefix,enabled,expires_at,
 created_at,last_used_at,revoked_at,created_by,label,token_ciphertext FROM subscription_tokens
	  WHERE id=? AND revoked_at IS NULL AND enabled=1
	    AND (expires_at IS NULL OR expires_at > ?)`, id, nowUnix())
	token, ciphertext, err := scanTokenWithCiphertext(row)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("client: reveal token: %w", err)
	}
	if ciphertext == "" {
		return "", ErrSecretUnavailable
	}
	// token_ciphertext is only ever written via sealToken; refuse the legacy
	// plaintext passthrough of Cipher.Decrypt (#1283) and any binding that
	// does not match this row, so a planted or re-pointed row cannot be
	// echoed back as a "revealed" token (#1281). The row cannot authenticate
	// either, so the operator gets the same "rotate it" answer as for a token
	// with no stored secret.
	plaintext, ok := s.openToken(token, ciphertext)
	if !ok {
		return "", fmt.Errorf("%w: stored token ciphertext is invalid", ErrSecretUnavailable)
	}
	return plaintext, nil
}

// --- transactional variants used by the unified mutation orchestration ---
// These run inside a caller-managed Tx so token writes commit atomically with
// the desired-revision bump and the immutable snapshot.

// issueTokenTx creates a token row inside the transaction, returning plaintext
// once. The row has no sealed issuance binding yet and does not authenticate
// until TokenStore seals it (RotateWithExpiry does so in the same
// transaction, #1281). Unexported so no caller can hand out an unsealed row.
func (t *Tx) issueTokenTx(clientID, label string, expiresAt *int64) (IssuedToken, error) {
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

// rotateTokenWithExpiryTx revokes the given token and issues a fresh,
// still unsealed one for the same client inside the transaction, so a token
// can never be revoked without its replacement being stored. Only
// TokenStore.RotateWithExpiry calls it; it seals the new row in the same
// transaction.
func (t *Tx) rotateTokenWithExpiryTx(id string, expiresAt *int64, expirySupplied bool) (IssuedToken, error) {
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
	return t.issueTokenTx(tok.ClientID, tok.Label, tok.ExpiresAt)
}

// ReencryptSubscriptionTokens moves every stored token reveal ciphertext to a
// replacement master cipher within the caller's transaction. Active and
// revoked rows are both covered so no live database ciphertext is stranded
// under the old key.
func (t *Tx) ReencryptSubscriptionTokens(oldCipher, newCipher *secrets.Cipher) error {
	if oldCipher == nil || newCipher == nil {
		return fmt.Errorf("client: token rotation ciphers unavailable")
	}
	oldBinding, err := TokenBindingCipher(oldCipher)
	if err != nil {
		return fmt.Errorf("client: derive token binding cipher for master-key rotation: %w", err)
	}
	newBinding, err := TokenBindingCipher(newCipher)
	if err != nil {
		return fmt.Errorf("client: derive token binding cipher for master-key rotation: %w", err)
	}
	rows, err := t.q.Query(`SELECT id, token_ciphertext FROM subscription_tokens WHERE token_ciphertext IS NOT NULL AND token_ciphertext<>'' ORDER BY id`)
	if err != nil {
		return fmt.Errorf("client: list tokens for master-key rotation: %w", err)
	}
	type encryptedToken struct {
		id    string
		value []byte
	}
	var tokens []encryptedToken
	for rows.Next() {
		var token encryptedToken
		if err := rows.Scan(&token.id, &token.value); err != nil {
			_ = rows.Close()
			return fmt.Errorf("client: scan token for master-key rotation: %w", err)
		}
		tokens = append(tokens, token)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("client: close token rotation rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("client: list tokens for master-key rotation: %w", err)
	}
	for _, token := range tokens {
		// Cipher.Decrypt passes non-"ve1:" input through verbatim. Re-sealing
		// that under the new key would launder a planted plaintext row into a
		// valid ciphertext that Reveal then accepts (#1283), so refuse it.
		if !secrets.IsEncrypted(string(token.value)) {
			return fmt.Errorf("client: token %s ciphertext is invalid; refusing master-key rotation (delete its subscription_tokens row, or revoke it and clear its token_ciphertext, then retry; revoking alone does not help because revoked rows are still rotated)", token.id)
		}
		// Issuance bindings (#1281) are sealed under the token subkey; rows
		// issued before them still carry a bare master-cipher seal, which
		// moves to the new master key unchanged in kind.
		to := newBinding
		plaintext, err := oldBinding.Decrypt(string(token.value))
		if err != nil {
			to = newCipher
			plaintext, err = oldCipher.Decrypt(string(token.value))
		}
		if err != nil {
			return fmt.Errorf("client: decrypt token %s for master-key rotation: %w", token.id, err)
		}
		encrypted, err := to.Encrypt(plaintext)
		if err != nil {
			return fmt.Errorf("client: encrypt token %s for master-key rotation: %w", token.id, err)
		}
		if _, err := t.q.Exec(`UPDATE subscription_tokens SET token_ciphertext=? WHERE id=?`, []byte(encrypted), token.id); err != nil {
			return fmt.Errorf("client: persist token %s master-key rotation: %w", token.id, err)
		}
	}
	return nil
}

func scanTokenWithCiphertext(row scanner) (SubscriptionToken, string, error) {
	var t SubscriptionToken
	var hashBytes []byte
	var enabled int
	var ciphertext sql.NullString
	err := row.Scan(&t.ID, &t.ClientID, &hashBytes, &t.Prefix, &enabled,
		&t.ExpiresAt, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt, &t.CreatedBy, &t.Label, &ciphertext)
	if err != nil {
		return SubscriptionToken{}, "", err
	}
	t.TokenHash = string(hashBytes)
	t.Enabled = enabled == 1
	return t, ciphertext.String, nil
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
