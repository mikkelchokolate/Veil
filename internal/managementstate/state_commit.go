package managementstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
)

const (
	pendingMutationJournalSuffix  = ".pending-mutation.json"
	pendingMutationPreviousSuffix = ".pending-mutation.previous"
	pendingMutationVersion        = 1
)

// PendingMutationJournal is the durable decision record for a Management
// state-file publication that has not yet been reconciled with SQLite.
type PendingMutationJournal struct {
	Version             int    `json:"version"`
	PreviousRevision    uint64 `json:"previousRevision"`
	IntendedRevision    uint64 `json:"intendedRevision"`
	PreviousStateExists bool   `json:"previousStateExists"`
	PreviousStateSHA256 string `json:"previousStateSha256,omitempty"`
	IntendedStateSHA256 string `json:"intendedStateSha256"`
}

// StateCommit owns the durable artifacts for one state-file/SQLite commit.
// Callers publish through PrepareStateCommit, then either Rollback after a
// failed SQLite transaction or Finalize after the matching transaction commits.
type StateCommit struct {
	store        Store
	journal      PendingMutationJournal
	journalPath  string
	previousPath string
}

// EncodedStateSHA256 returns the lowercase SHA-256 digest of exact state.json
// bytes. The digest intentionally binds the serialized/encrypted file, not a
// re-marshaled logical representation.
func EncodedStateSHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// PrepareStateCommit durably saves the previous state and a decision journal,
// then atomically publishes the intended encoded state file. The caller must
// hold the Management snapshot barrier until SQLite is committed and this
// object is finalized or rolled back.
func (s Store) PrepareStateCommit(encoded []byte, previousRevision, intendedRevision uint64) (*StateCommit, error) {
	if s.path == "" {
		return nil, errors.New("state commit requires a persistent state path")
	}
	if previousRevision == math.MaxUint64 || intendedRevision != previousRevision+1 {
		return nil, fmt.Errorf("invalid state commit revision boundary %d -> %d", previousRevision, intendedRevision)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return nil, err
	}
	journalPath := s.path + pendingMutationJournalSuffix
	if _, err := os.Lstat(journalPath); err == nil {
		return nil, errors.New("pending state mutation journal already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect pending state mutation journal: %w", err)
	}
	previous, previousExists, err := readOptionalStateFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read previous management state: %w", err)
	}
	commit := &StateCommit{
		store:        s,
		journalPath:  journalPath,
		previousPath: s.path + pendingMutationPreviousSuffix,
		journal: PendingMutationJournal{
			Version:             pendingMutationVersion,
			PreviousRevision:    previousRevision,
			IntendedRevision:    intendedRevision,
			PreviousStateExists: previousExists,
			IntendedStateSHA256: EncodedStateSHA256(encoded),
		},
	}
	if previousExists {
		commit.journal.PreviousStateSHA256 = EncodedStateSHA256(previous)
		if err := writeStoreFileAtomic(commit.previousPath, previous, nil); err != nil {
			return nil, fmt.Errorf("save previous management state: %w", err)
		}
	} else if err := removeStateCommitFile(commit.previousPath); err != nil {
		return nil, fmt.Errorf("remove orphaned previous management state: %w", err)
	}
	journalBody, err := json.Marshal(commit.journal)
	if err != nil {
		_ = removeStateCommitFile(commit.previousPath)
		return nil, fmt.Errorf("marshal state mutation journal: %w", err)
	}
	if err := writeStoreFileAtomic(commit.journalPath, append(journalBody, '\n'), nil); err != nil {
		_ = removeStateCommitFile(commit.previousPath)
		return nil, fmt.Errorf("write state mutation journal: %w", err)
	}
	if err := s.SaveEncoded(encoded); err != nil {
		rollbackErr := commit.Rollback()
		if rollbackErr != nil {
			return nil, errors.Join(err, fmt.Errorf("rollback failed state publication: %w", rollbackErr))
		}
		return nil, err
	}
	return commit, nil
}

// LoadPendingStateCommit loads and validates a durable mutation journal. An
// orphaned previous-state copy with no journal is safe to remove at startup.
func (s Store) LoadPendingStateCommit() (*StateCommit, bool, error) {
	if s.path == "" {
		return nil, false, nil
	}
	journalPath := s.path + pendingMutationJournalSuffix
	previousPath := s.path + pendingMutationPreviousSuffix
	body, err := os.ReadFile(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		if err := removeStateCommitFile(previousPath); err != nil {
			return nil, false, fmt.Errorf("remove orphaned state commit copy: %w", err)
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read state mutation journal: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var journal PendingMutationJournal
	if err := decoder.Decode(&journal); err != nil {
		return nil, false, fmt.Errorf("decode state mutation journal: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, false, fmt.Errorf("decode state mutation journal: %w", err)
	}
	if err := validatePendingMutationJournal(journal); err != nil {
		return nil, false, err
	}
	return &StateCommit{store: s, journal: journal, journalPath: journalPath, previousPath: previousPath}, true, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("unexpected data after journal")
}

func validatePendingMutationJournal(journal PendingMutationJournal) error {
	if journal.Version != pendingMutationVersion {
		return fmt.Errorf("unsupported state mutation journal version %d", journal.Version)
	}
	if journal.PreviousRevision == math.MaxUint64 || journal.IntendedRevision != journal.PreviousRevision+1 {
		return fmt.Errorf("invalid state mutation journal revision boundary %d -> %d", journal.PreviousRevision, journal.IntendedRevision)
	}
	if !validStateDigest(journal.IntendedStateSHA256) {
		return errors.New("state mutation journal intended digest is invalid")
	}
	if journal.PreviousStateExists != validStateDigest(journal.PreviousStateSHA256) {
		return errors.New("state mutation journal previous-state digest is inconsistent")
	}
	return nil
}

func validStateDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

// Journal returns a copy of the durable commit decision record.
func (c *StateCommit) Journal() PendingMutationJournal { return c.journal }

// CurrentStateDigest returns the digest and existence of the published state.
func (c *StateCommit) CurrentStateDigest() (string, bool, error) {
	body, exists, err := readOptionalStateFile(c.store.path)
	if err != nil || !exists {
		return "", exists, err
	}
	return EncodedStateSHA256(body), true, nil
}

// Rollback restores the exact previous file (or absence), verifies the result,
// and removes the durable recovery artifacts.
func (c *StateCommit) Rollback() error {
	if c == nil {
		return errors.New("state commit is nil")
	}
	if c.journal.PreviousStateExists {
		previous, err := os.ReadFile(c.previousPath)
		if err != nil {
			return fmt.Errorf("read previous management state copy: %w", err)
		}
		if got := EncodedStateSHA256(previous); got != c.journal.PreviousStateSHA256 {
			return fmt.Errorf("previous management state digest mismatch: journal=%s file=%s", c.journal.PreviousStateSHA256, got)
		}
		if err := c.store.RestoreEncoded(previous, true); err != nil {
			return err
		}
	} else if err := c.store.RestoreEncoded(nil, false); err != nil {
		return err
	}
	currentDigest, currentExists, err := c.CurrentStateDigest()
	if err != nil {
		return err
	}
	if currentExists != c.journal.PreviousStateExists || currentDigest != c.journal.PreviousStateSHA256 {
		return errors.New("restored management state does not match the journal")
	}
	return c.cleanup()
}

// Finalize verifies that the published file is the intended state and removes
// recovery artifacts after the matching SQLite transaction has committed.
func (c *StateCommit) Finalize() error {
	if c == nil {
		return errors.New("state commit is nil")
	}
	currentDigest, currentExists, err := c.CurrentStateDigest()
	if err != nil {
		return err
	}
	if !currentExists || currentDigest != c.journal.IntendedStateSHA256 {
		return fmt.Errorf("published management state does not match intended digest %s", c.journal.IntendedStateSHA256)
	}
	return c.cleanup()
}

func (c *StateCommit) cleanup() error {
	if err := removeStateCommitFile(c.journalPath); err != nil {
		return fmt.Errorf("remove state mutation journal: %w", err)
	}
	if err := removeStateCommitFile(c.previousPath); err != nil {
		return fmt.Errorf("remove previous management state copy: %w", err)
	}
	bestEffortSyncStoreDirectory(filepath.Dir(c.store.path))
	return nil
}

func readOptionalStateFile(path string) ([]byte, bool, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return body, err == nil, err
}

func removeStateCommitFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
