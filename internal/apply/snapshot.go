package apply

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mikkelchokolate/Veil/internal/secrets"
)

// SnapshotStore persists an immutable, content-addressed-by-revision snapshot
// of the desired configuration for each desired revision. Once a revision is
// recorded its snapshot never changes, so an apply job for revision N always
// renders exactly the configuration that was committed as revision N — never
// a newer mutable state.
//
// Once armed with the state cipher (SetCipher), every read authenticates the
// row against a MAC keyed from the state key (#1280): a veil.db writer who
// does not hold /etc/veil/state.key cannot author or edit a snapshot that the
// /s/ feed, the apply render path or rollback will accept.
type SnapshotStore struct {
	db *sql.DB

	mu   sync.RWMutex
	seal *SnapshotSeal
}

func NewSnapshotStore(db *sql.DB) *SnapshotStore { return &SnapshotStore{db: db} }

// ErrSnapshotUnauthenticated reports a revision_snapshots row whose
// payload_mac does not authenticate the row under the current state key: it
// was edited or planted by a veil.db writer, or it is a pre-#1280 row that
// was never sealed.
var ErrSnapshotUnauthenticated = errors.New("apply: revision snapshot is not authenticated by the state key")

// snapshotMACLabel derives the snapshot MAC subkey. The subkey is
// HMAC-SHA256(master key, label), a construction distinct from the
// sha256(key || label) used by secrets.DeriveCipher/DeriveToken, so no other
// derivation can print or reuse this key.
const snapshotMACLabel = "veil-revision-snapshot-mac-v1"

// snapshotMACPrefix versions the stored payload_mac value.
const snapshotMACPrefix = "sm1:"

// SnapshotSeal computes and verifies revision snapshot MACs.
type SnapshotSeal struct{ key []byte }

// NewSnapshotSeal derives the snapshot MAC key from the state master cipher.
func NewSnapshotSeal(master *secrets.Cipher) (*SnapshotSeal, error) {
	if master == nil {
		return nil, errors.New("apply: snapshot seal requires the state cipher")
	}
	mac := hmac.New(sha256.New, master.KeyBytes())
	mac.Write([]byte(snapshotMACLabel))
	return &SnapshotSeal{key: mac.Sum(nil)}, nil
}

// MAC authenticates every column a reader trusts: the revision it is filed
// under, its creation time (the legacy effective time), the state-file
// digest it is bound to, and the exact payload bytes.
func (s *SnapshotSeal) MAC(revision uint64, createdAt int64, stateSHA256 string, payload []byte) string {
	mac := hmac.New(sha256.New, s.key)
	var header [8]byte
	mac.Write([]byte(snapshotMACLabel))
	mac.Write([]byte{0})
	binary.BigEndian.PutUint64(header[:], revision)
	mac.Write(header[:])
	binary.BigEndian.PutUint64(header[:], uint64(createdAt))
	mac.Write(header[:])
	binary.BigEndian.PutUint64(header[:], uint64(len(stateSHA256)))
	mac.Write(header[:])
	mac.Write([]byte(stateSHA256))
	binary.BigEndian.PutUint64(header[:], uint64(len(payload)))
	mac.Write(header[:])
	mac.Write(payload)
	return snapshotMACPrefix + hex.EncodeToString(mac.Sum(nil))
}

// Verify reports whether stored is a valid MAC for the row.
func (s *SnapshotSeal) Verify(revision uint64, createdAt int64, stateSHA256 string, payload []byte, stored string) bool {
	if s == nil || !strings.HasPrefix(stored, snapshotMACPrefix) {
		return false
	}
	want := s.MAC(revision, createdAt, stateSHA256, payload)
	return hmac.Equal([]byte(want), []byte(stored))
}

// SetCipher arms authentication: from then on Load, EffectiveAt and the
// store's own Save paths require/produce sealed rows under cipher. Call it
// again after a state-key rotation. A nil cipher is refused, so an armed
// store can never silently fall back to unauthenticated reads.
func (s *SnapshotStore) SetCipher(cipher *secrets.Cipher) error {
	seal, err := NewSnapshotSeal(cipher)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.seal = seal
	s.mu.Unlock()
	return nil
}

// Seal returns the store's current seal, or nil when the store was never
// armed (unpersisted in-memory panels and offline tooling).
func (s *SnapshotStore) Seal() *SnapshotSeal {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.seal
}

// Save records the snapshot payload for a revision. It is retained for
// unpersisted/legacy callers that have no state-file digest.
func (s *SnapshotStore) Save(revision uint64, payload []byte) error {
	return SaveSnapshotTx(s.db, s.Seal(), revision, payload)
}

// SaveBound records a snapshot together with the SHA-256 digest of the exact
// state.json bytes that belong to the same desired revision.
func (s *SnapshotStore) SaveBound(revision uint64, payload []byte, stateSHA256 string) error {
	return SaveSnapshotTxBound(s.db, s.Seal(), revision, payload, stateSHA256)
}

// SaveSnapshotTx is Save inside a caller-managed transaction. A nil seal
// writes an unsealed row, which an armed store refuses to read; it exists for
// unpersisted panels that run without a state key.
func SaveSnapshotTx(q DBTX, seal *SnapshotSeal, revision uint64, payload []byte) error {
	return saveSnapshotTx(q, seal, revision, payload, "", false)
}

// SaveSnapshotTxBound commits an immutable snapshot and its state-file digest
// inside the caller's revision transaction, sealed under seal.
func SaveSnapshotTxBound(q DBTX, seal *SnapshotSeal, revision uint64, payload []byte, stateSHA256 string) error {
	if seal == nil {
		return fmt.Errorf("apply: snapshot seal for revision %d is unavailable", revision)
	}
	return saveSnapshotTx(q, seal, revision, payload, stateSHA256, true)
}

func saveSnapshotTx(q DBTX, seal *SnapshotSeal, revision uint64, payload []byte, stateSHA256 string, requireDigest bool) error {
	if requireDigest && !validStateSHA256(stateSHA256) {
		return fmt.Errorf("apply: state digest for revision %d is invalid", revision)
	}
	createdAt := time.Now().Unix()
	mac := ""
	if seal != nil {
		mac = seal.MAC(revision, createdAt, stateSHA256, payload)
	}
	if _, err := q.Exec(
		`INSERT OR IGNORE INTO revision_snapshots(revision, payload, state_sha256, created_at, payload_mac) VALUES(?, ?, ?, ?, ?)`,
		revision, string(payload), stateSHA256, createdAt, mac,
	); err != nil {
		return fmt.Errorf("apply: save snapshot rev %d: %w", revision, err)
	}
	if !requireDigest {
		return nil
	}
	// INSERT OR IGNORE preserves first-write immutability, but a bound writer
	// must not hide a conflicting stale (or planted, #1280) row at the same
	// revision.
	var storedPayload, storedDigest, storedMAC string
	var storedCreatedAt int64
	if err := q.QueryRow(
		`SELECT payload, state_sha256, created_at, payload_mac FROM revision_snapshots WHERE revision = ?`, revision,
	).Scan(&storedPayload, &storedDigest, &storedCreatedAt, &storedMAC); err != nil {
		return fmt.Errorf("apply: verify snapshot rev %d: %w", revision, err)
	}
	if storedPayload != string(payload) || storedDigest != stateSHA256 ||
		!seal.Verify(revision, storedCreatedAt, storedDigest, []byte(storedPayload), storedMAC) {
		return fmt.Errorf("apply: immutable snapshot collision for revision %d", revision)
	}
	return nil
}

type snapshotRow struct {
	payload     string
	stateSHA256 string
	createdAt   int64
	mac         string
}

func (s *SnapshotStore) loadRow(revision uint64) (snapshotRow, error) {
	var row snapshotRow
	err := s.db.QueryRow(
		`SELECT payload, state_sha256, created_at, payload_mac FROM revision_snapshots WHERE revision = ?`, revision,
	).Scan(&row.payload, &row.stateSHA256, &row.createdAt, &row.mac)
	if err == sql.ErrNoRows {
		return row, fmt.Errorf("apply: no snapshot for revision %d", revision)
	}
	if err != nil {
		return row, fmt.Errorf("apply: load snapshot rev %d: %w", revision, err)
	}
	return row, nil
}

// loadAuthenticated reads a row and, when the store is armed, refuses it
// unless its MAC verifies.
func (s *SnapshotStore) loadAuthenticated(revision uint64) (snapshotRow, error) {
	row, err := s.loadRow(revision)
	if err != nil {
		return row, err
	}
	if seal := s.Seal(); seal != nil {
		if !seal.Verify(revision, row.createdAt, row.stateSHA256, []byte(row.payload), row.mac) {
			if row.mac == "" {
				return snapshotRow{}, fmt.Errorf("%w: revision %d is an unsealed (pre-#1280) row", ErrSnapshotUnauthenticated, revision)
			}
			return snapshotRow{}, fmt.Errorf("%w: revision %d payload MAC mismatch", ErrSnapshotUnauthenticated, revision)
		}
	}
	return row, nil
}

// Load returns the snapshot payload for a revision. An armed store returns
// only rows that authenticate under the state key.
func (s *SnapshotStore) Load(revision uint64) ([]byte, error) {
	row, err := s.loadAuthenticated(revision)
	if err != nil {
		return nil, err
	}
	return []byte(row.payload), nil
}

// LoadUnsealed returns the payload of a revision that carries no MAC yet, for
// the startup pass that proves a pre-#1280 desired row against state.json
// before sealing it. It refuses sealed rows so callers cannot use it to skip
// authentication.
func (s *SnapshotStore) LoadUnsealed(revision uint64) ([]byte, error) {
	row, err := s.loadRow(revision)
	if err != nil {
		return nil, err
	}
	if row.mac != "" {
		return nil, fmt.Errorf("apply: snapshot rev %d is already sealed", revision)
	}
	return []byte(row.payload), nil
}

// IsSealed reports whether a revision's row carries a MAC (valid or not).
func (s *SnapshotStore) IsSealed(revision uint64) (bool, error) {
	row, err := s.loadRow(revision)
	if err != nil {
		return false, err
	}
	return row.mac != "", nil
}

// SealLegacy seals a pre-#1280 row whose payload the caller has proven
// authentic. The update only lands if the row still holds exactly the
// verified payload and state digest and is still unsealed.
func (s *SnapshotStore) SealLegacy(revision uint64, verifiedPayload []byte, stateSHA256 string) error {
	seal := s.Seal()
	if seal == nil {
		return fmt.Errorf("apply: snapshot seal for revision %d is unavailable", revision)
	}
	row, err := s.loadRow(revision)
	if err != nil {
		return err
	}
	if row.mac != "" || row.payload != string(verifiedPayload) || row.stateSHA256 != stateSHA256 {
		return fmt.Errorf("apply: legacy snapshot rev %d changed before sealing", revision)
	}
	mac := seal.MAC(revision, row.createdAt, row.stateSHA256, verifiedPayload)
	result, err := s.db.Exec(
		`UPDATE revision_snapshots SET payload_mac = ? WHERE revision = ? AND payload_mac = '' AND payload = ? AND state_sha256 = ? AND created_at = ?`,
		mac, revision, string(verifiedPayload), stateSHA256, row.createdAt,
	)
	if err != nil {
		return fmt.Errorf("apply: seal legacy snapshot rev %d: %w", revision, err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("apply: legacy snapshot rev %d changed before sealing", revision)
	}
	return nil
}

// ResealStateDigest moves an authenticated row from one state.json digest to
// another and re-seals it. Startup uses it when it rewrites a pre-#1280
// state.json at SnapshotSealSchemaVersion: the payload is unchanged and only
// the bound file bytes change. The row must authenticate under the current
// seal and still carry fromSHA256; the update is a compare-and-set on the
// verified MAC.
func (s *SnapshotStore) ResealStateDigest(revision uint64, fromSHA256, toSHA256 string) error {
	seal := s.Seal()
	if seal == nil {
		return fmt.Errorf("apply: snapshot seal for revision %d is unavailable", revision)
	}
	if !validStateSHA256(toSHA256) {
		return fmt.Errorf("apply: state digest for revision %d is invalid", revision)
	}
	row, err := s.loadAuthenticated(revision)
	if err != nil {
		return err
	}
	if row.stateSHA256 != fromSHA256 {
		return fmt.Errorf("apply: snapshot rev %d is not bound to the expected state digest", revision)
	}
	mac := seal.MAC(revision, row.createdAt, toSHA256, []byte(row.payload))
	result, err := s.db.Exec(
		`UPDATE revision_snapshots SET state_sha256 = ?, payload_mac = ? WHERE revision = ? AND payload_mac = ? AND payload = ? AND state_sha256 = ? AND created_at = ?`,
		toSHA256, mac, revision, row.mac, row.payload, fromSHA256, row.createdAt,
	)
	if err != nil {
		return fmt.Errorf("apply: reseal snapshot rev %d: %w", revision, err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return fmt.Errorf("apply: snapshot rev %d changed before resealing", revision)
	}
	return nil
}

// EffectiveAt returns the immutable row creation time used to evaluate legacy
// snapshots written before effectiveAt became part of the JSON payload. The
// creation time is covered by the row MAC.
func (s *SnapshotStore) EffectiveAt(revision uint64) (int64, error) {
	row, err := s.loadAuthenticated(revision)
	if err != nil {
		return 0, fmt.Errorf("apply: load effective time rev %d: %w", revision, err)
	}
	return row.createdAt, nil
}

// Has reports whether a snapshot exists for a revision.
func (s *SnapshotStore) Has(revision uint64) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM revision_snapshots WHERE revision = ?`, revision).Scan(&n)
	return n > 0
}

// StateDigest returns the exact state.json digest associated with revision.
func (s *SnapshotStore) StateDigest(revision uint64) (string, error) {
	var digest string
	if err := s.db.QueryRow(
		`SELECT state_sha256 FROM revision_snapshots WHERE revision = ?`, revision,
	).Scan(&digest); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("apply: no snapshot for revision %d", revision)
		}
		return "", fmt.Errorf("apply: load state digest rev %d: %w", revision, err)
	}
	return digest, nil
}

// BindStateDigest fills the one-time association for a proven-consistent
// legacy snapshot. It never replaces an existing non-empty digest, and never
// touches a sealed row (its MAC covers the digest).
func (s *SnapshotStore) BindStateDigest(revision uint64, stateSHA256 string) error {
	if !validStateSHA256(stateSHA256) {
		return fmt.Errorf("apply: state digest for revision %d is invalid", revision)
	}
	if _, err := s.db.Exec(
		`UPDATE revision_snapshots SET state_sha256 = ? WHERE revision = ? AND state_sha256 = '' AND payload_mac = ''`,
		stateSHA256, revision,
	); err != nil {
		return fmt.Errorf("apply: bind state digest rev %d: %w", revision, err)
	}
	stored, err := s.StateDigest(revision)
	if err != nil {
		return err
	}
	if stored != stateSHA256 {
		return fmt.Errorf("apply: state digest mismatch for revision %d", revision)
	}
	return nil
}

func validStateSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
