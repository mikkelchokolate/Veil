package statecommit

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

const (
	keyRotationJournalVersion = 1
	keyRotationJournalSuffix  = ".pending-key-rotation.json"
	keyRotationIntendedSuffix = ".pending-key-rotation.intended"
)

type keyRotationPhase string

const (
	rotationPhasePrepared        keyRotationPhase = "prepared"
	rotationPhaseKeyPublished    keyRotationPhase = "key-published"
	rotationPhaseStatePublished  keyRotationPhase = "state-published"
	rotationPhaseSQLiteCommitted keyRotationPhase = "sqlite-committed"
)

var errKeyRotationInterrupted = errors.New("state commit: simulated key-rotation interruption")

type uncertainRotationCommitError struct{ err error }

func (e *uncertainRotationCommitError) Error() string { return e.err.Error() }
func (e *uncertainRotationCommitError) Unwrap() error { return e.err }

// RotateKeyOptions identifies every durable file participating in one key
// rotation. KeyPath is the authoritative key that decrypts the current state;
// TargetKeyPath defaults to KeyPath and supports the CLI's explicit destination
// mode. Production callers leave interruptAfter unset.
type RotateKeyOptions struct {
	StatePath      string
	KeyPath        string
	TargetKeyPath  string
	DatabasePath   string
	Now            func() time.Time
	KeepSafetyCopy bool

	interruptAfter keyRotationPhase
}

// RecoverKeyRotationOptions identifies the state and SQLite stores needed to
// resolve a durable key-rotation journal before any cipher is constructed.
type RecoverKeyRotationOptions struct {
	StatePath    string
	DatabasePath string
}

// KeyRotationResult reports the operator-visible safety copies retained by a
// successful privileged rotation.
type KeyRotationResult struct {
	SafetyStatePath string
	SafetyKeyPath   string
	Revision        uint64
}

type keyRotationJournal struct {
	Version int              `json:"version"`
	Phase   keyRotationPhase `json:"phase"`

	LiveStatePath string `json:"liveStatePath"`
	SourceKeyPath string `json:"sourceKeyPath"`
	LiveKeyPath   string `json:"liveKeyPath"`

	PreviousRevision uint64 `json:"previousRevision"`
	IntendedRevision uint64 `json:"intendedRevision"`

	PreviousKeyExists       bool   `json:"previousKeyExists"`
	PreviousKeySHA256       string `json:"previousKeySha256,omitempty"`
	IntendedKeySHA256       string `json:"intendedKeySha256"`
	SourceKeySHA256         string `json:"sourceKeySha256"`
	PreviousTargetKeyExists bool   `json:"previousTargetKeyExists,omitempty"`
	PreviousTargetKeySHA256 string `json:"previousTargetKeySha256,omitempty"`

	PreviousStateSHA256 string `json:"previousStateSha256"`
	IntendedStateSHA256 string `json:"intendedStateSha256"`

	PreviousKeyPath       string `json:"previousKeyPath"`
	PreviousTargetKeyPath string `json:"previousTargetKeyPath,omitempty"`
	IntendedKeyPath       string `json:"intendedKeyPath"`
	PreviousStatePath     string `json:"previousStatePath"`
	IntendedStatePath     string `json:"intendedStatePath"`

	PreviousKeyMode       uint32 `json:"previousKeyMode"`
	PreviousKeyUID        int    `json:"previousKeyUid"`
	PreviousKeyGID        int    `json:"previousKeyGid"`
	PreviousTargetKeyMode uint32 `json:"previousTargetKeyMode,omitempty"`
	PreviousTargetKeyUID  int    `json:"previousTargetKeyUid,omitempty"`
	PreviousTargetKeyGID  int    `json:"previousTargetKeyGid,omitempty"`

	PreviousStateMode uint32 `json:"previousStateMode"`
	PreviousStateUID  int    `json:"previousStateUid"`
	PreviousStateGID  int    `json:"previousStateGid"`

	IntendedKeyMode uint32 `json:"intendedKeyMode"`
	IntendedKeyUID  int    `json:"intendedKeyUid"`
	IntendedKeyGID  int    `json:"intendedKeyGid"`

	KeepSafetyCopy bool `json:"keepSafetyCopy,omitempty"`
}

type rotationFileMetadata struct {
	mode os.FileMode
	uid  int
	gid  int
}

// KeyRotationJournalPath returns the durable decision-record path for state.
func KeyRotationJournalPath(statePath string) string {
	return statePath + keyRotationJournalSuffix
}

// RotateKey performs key publication, state publication, desired-revision
// commit and journal finalization while holding the cross-process snapshot
// barrier from the authoritative read through the final durable decision.
func RotateKey(options RotateKeyOptions) (KeyRotationResult, error) {
	var result KeyRotationResult
	if options.StatePath == "" || options.KeyPath == "" {
		return result, errors.New("state commit: state and key paths are required for key rotation")
	}
	if options.TargetKeyPath == "" {
		options.TargetKeyPath = options.KeyPath
	}
	if options.DatabasePath == "" {
		options.DatabasePath = filepath.Join(filepath.Dir(options.StatePath), "veil.db")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	err := managementstate.WithSnapshotBarrier(options.StatePath, func() error {
		if err := recoverKeyRotationLocked(RecoverKeyRotationOptions{
			StatePath: options.StatePath, DatabasePath: options.DatabasePath,
		}); err != nil {
			return fmt.Errorf("state commit: recover previous key rotation: %w", err)
		}
		var err error
		result, err = rotateKeyLocked(options)
		return err
	})
	return result, err
}

func rotateKeyLocked(options RotateKeyOptions) (KeyRotationResult, error) {
	var result KeyRotationResult
	if _, ok, err := managementstate.NewStore(options.StatePath, nil).LoadPendingStateCommit(); err != nil {
		return result, err
	} else if ok {
		return result, errors.New("state commit: pending state mutation must be recovered before key rotation")
	}

	sourceKey, sourceKeyMeta, err := readRotationFile(options.KeyPath, 0)
	if err != nil {
		return result, fmt.Errorf("state commit: read old key file: %w", err)
	}
	if len(sourceKey) != secrets.KeySize {
		return result, fmt.Errorf("state commit: old key file has wrong length: %d bytes (expected %d)", len(sourceKey), secrets.KeySize)
	}
	oldCipher, err := cipherForKey(sourceKey)
	if err != nil {
		return result, err
	}
	previousState, stateMeta, err := readRotationFile(options.StatePath, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, fmt.Errorf("state commit: no state found at %s", options.StatePath)
		}
		return result, fmt.Errorf("state commit: read current state: %w", err)
	}
	store := managementstate.NewStore(options.StatePath, oldCipher)
	snapshot, ok, err := store.Load()
	if err != nil {
		return result, fmt.Errorf("state commit: decrypt current state: %w", err)
	}
	if !ok {
		return result, fmt.Errorf("state commit: no state found at %s", options.StatePath)
	}

	db, err := storage.OpenExisting(options.DatabasePath)
	if err != nil {
		return result, fmt.Errorf("state commit: open rotation database: %w", err)
	}
	defer db.Close()
	revisions, err := apply.NewRevisionStore(db).Get()
	if err != nil {
		return result, fmt.Errorf("state commit: read rotation revisions: %w", err)
	}
	if revisions.Desired > 0 {
		boundDigest, err := apply.NewSnapshotStore(db).StateDigest(revisions.Desired)
		if err != nil {
			return result, fmt.Errorf("state commit: read current revision state digest: %w", err)
		}
		if boundDigest == "" || boundDigest != digestBytes(previousState) {
			return result, fmt.Errorf("state commit: current state does not match desired revision %d", revisions.Desired)
		}
	}
	if revisions.Desired == math.MaxUint64 {
		return result, errors.New("state commit: desired revision exhausted")
	}

	intendedKey := make([]byte, secrets.KeySize)
	if _, err := rand.Read(intendedKey); err != nil {
		return result, fmt.Errorf("state commit: generate intended key: %w", err)
	}
	newCipher, err := cipherForKey(intendedKey)
	if err != nil {
		return result, err
	}
	intendedState, err := managementstate.NewStore(options.StatePath, newCipher).Marshal(snapshot)
	if err != nil {
		return result, fmt.Errorf("state commit: encode intended state: %w", err)
	}

	sameKeyPath := filepath.Clean(options.TargetKeyPath) == filepath.Clean(options.KeyPath)
	var previousTargetKey []byte
	var previousTargetKeyExists bool
	previousTargetKeyMeta := rotationFileMetadata{uid: -1, gid: -1}
	if !sameKeyPath {
		previousTargetKey, previousTargetKeyExists, previousTargetKeyMeta, err = readOptionalRotationFile(options.TargetKeyPath, secrets.KeySize)
		if err != nil {
			return result, fmt.Errorf("state commit: read previous target key: %w", err)
		}
	}
	intendedKeyMeta := sourceKeyMeta
	if previousTargetKeyExists {
		intendedKeyMeta = previousTargetKeyMeta
	}
	suffix := options.Now().UTC().Format("20060102T150405.000000000Z")
	journal := keyRotationJournal{
		Version: keyRotationJournalVersion, Phase: rotationPhasePrepared,
		LiveStatePath: options.StatePath, SourceKeyPath: options.KeyPath, LiveKeyPath: options.TargetKeyPath,
		PreviousRevision: revisions.Desired, IntendedRevision: revisions.Desired + 1,
		PreviousKeyExists: true, PreviousKeySHA256: digestBytes(sourceKey),
		IntendedKeySHA256: digestBytes(intendedKey), SourceKeySHA256: digestBytes(sourceKey),
		PreviousTargetKeyExists: previousTargetKeyExists, PreviousTargetKeySHA256: digestIfPresent(previousTargetKey, previousTargetKeyExists),
		PreviousStateSHA256: digestBytes(previousState), IntendedStateSHA256: digestBytes(intendedState),
		PreviousKeyPath:       options.KeyPath + ".pre-rotation-" + suffix,
		PreviousTargetKeyPath: options.TargetKeyPath + ".pre-rotation-target-" + suffix,
		IntendedKeyPath:       options.TargetKeyPath + keyRotationIntendedSuffix,
		PreviousStatePath:     options.StatePath + ".pre-rotation-" + suffix,
		IntendedStatePath:     options.StatePath + keyRotationIntendedSuffix,
		PreviousKeyMode:       uint32(sourceKeyMeta.mode.Perm()), PreviousKeyUID: sourceKeyMeta.uid, PreviousKeyGID: sourceKeyMeta.gid,
		PreviousTargetKeyMode: uint32(previousTargetKeyMeta.mode.Perm()), PreviousTargetKeyUID: previousTargetKeyMeta.uid, PreviousTargetKeyGID: previousTargetKeyMeta.gid,
		PreviousStateMode: uint32(stateMeta.mode.Perm()), PreviousStateUID: stateMeta.uid, PreviousStateGID: stateMeta.gid,
		IntendedKeyMode: uint32(intendedKeyMeta.mode.Perm()), IntendedKeyUID: intendedKeyMeta.uid, IntendedKeyGID: intendedKeyMeta.gid,
		KeepSafetyCopy: options.KeepSafetyCopy,
	}
	if !previousTargetKeyExists {
		journal.PreviousTargetKeyMode, journal.PreviousTargetKeyUID, journal.PreviousTargetKeyGID = 0, -1, -1
	}
	if err := validateKeyRotationJournal(journal, options.StatePath); err != nil {
		return result, err
	}
	if err := writeRotationFile(journal.PreviousKeyPath, sourceKey, sourceKeyMeta); err != nil {
		return result, fmt.Errorf("state commit: save previous source key: %w", err)
	}
	if previousTargetKeyExists {
		if err := writeRotationFile(journal.PreviousTargetKeyPath, previousTargetKey, previousTargetKeyMeta); err != nil {
			return result, fmt.Errorf("state commit: save previous target key: %w", err)
		}
	}
	if err := writeRotationFile(journal.PreviousStatePath, previousState, stateMeta); err != nil {
		return result, fmt.Errorf("state commit: save previous state: %w", err)
	}
	if err := writeRotationFile(journal.IntendedKeyPath, intendedKey, intendedKeyMeta); err != nil {
		return result, fmt.Errorf("state commit: stage intended key: %w", err)
	}
	if err := writeRotationFile(journal.IntendedStatePath, intendedState, stateMeta); err != nil {
		return result, fmt.Errorf("state commit: stage intended state: %w", err)
	}
	if err := writeKeyRotationJournal(journal); err != nil {
		return result, fmt.Errorf("state commit: write key-rotation journal: %w", err)
	}
	result = KeyRotationResult{SafetyStatePath: journal.PreviousStatePath, SafetyKeyPath: journal.PreviousKeyPath}
	if options.interruptAfter == rotationPhasePrepared {
		return result, errKeyRotationInterrupted
	}

	rollback := func(cause error) (KeyRotationResult, error) {
		if rollbackErr := rollbackKeyRotation(journal); rollbackErr != nil {
			return result, errors.Join(cause, fmt.Errorf("state commit: rollback key rotation: %w", rollbackErr))
		}
		return result, cause
	}
	if err := writeRotationFile(journal.LiveKeyPath, intendedKey, intendedKeyMeta); err != nil {
		return rollback(fmt.Errorf("state commit: publish intended key: %w", err))
	}
	journal.Phase = rotationPhaseKeyPublished
	if err := writeKeyRotationJournal(journal); err != nil {
		return rollback(fmt.Errorf("state commit: record key publication: %w", err))
	}
	if options.interruptAfter == rotationPhaseKeyPublished {
		return result, errKeyRotationInterrupted
	}
	if err := writeRotationFile(journal.LiveStatePath, intendedState, stateMeta); err != nil {
		return rollback(fmt.Errorf("state commit: publish intended state: %w", err))
	}
	journal.Phase = rotationPhaseStatePublished
	if err := writeKeyRotationJournal(journal); err != nil {
		return rollback(fmt.Errorf("state commit: record state publication: %w", err))
	}
	if options.interruptAfter == rotationPhaseStatePublished {
		return result, errKeyRotationInterrupted
	}

	revision, err := commitRotationRevision(db, journal, snapshot, oldCipher, newCipher)
	if err != nil {
		var uncertain *uncertainRotationCommitError
		if !errors.As(err, &uncertain) {
			return rollback(err)
		}
		current, readErr := apply.NewRevisionStore(db).Get()
		if readErr != nil {
			return result, errors.Join(err, fmt.Errorf("state commit: resolve uncertain key-rotation commit: %w", readErr))
		}
		switch current.Desired {
		case journal.PreviousRevision:
			return rollback(err)
		case journal.IntendedRevision:
			result.Revision = journal.IntendedRevision
			journal.Phase = rotationPhaseSQLiteCommitted
			if phaseErr := writeKeyRotationJournal(journal); phaseErr != nil {
				log.Printf("state commit: uncertain key rotation committed but phase update failed: %v", phaseErr)
			}
			if finalizeErr := finalizeKeyRotation(db, journal); finalizeErr != nil {
				log.Printf("state commit: uncertain key rotation committed and left recovery journal: %v", finalizeErr)
			}
			return result, nil
		default:
			return result, errors.Join(err, fmt.Errorf("state commit: uncertain key-rotation commit left desired revision %d", current.Desired))
		}
	}
	result.Revision = revision
	journal.Phase = rotationPhaseSQLiteCommitted
	if err := writeKeyRotationJournal(journal); err != nil {
		log.Printf("state commit: committed key rotation revision %d left pre-commit journal phase: %v", revision, err)
		return result, nil
	}
	if options.interruptAfter == rotationPhaseSQLiteCommitted {
		return result, errKeyRotationInterrupted
	}
	if err := finalizeKeyRotation(db, journal); err != nil {
		log.Printf("state commit: committed key rotation revision %d left recovery journal: %v", revision, err)
	}
	return result, nil
}

// RecoverKeyRotation resolves a pending rotation before a caller loads the key
// or encrypted state. It never creates a missing database or key.
func RecoverKeyRotation(options RecoverKeyRotationOptions) error {
	return WithKeyRotationRecovery(options, nil)
}

// WithKeyRotationRecovery holds the snapshot barrier while resolving any
// pending rotation and while callback reads the now-coherent key/state pair.
// The callback must not acquire the same snapshot barrier.
func WithKeyRotationRecovery(options RecoverKeyRotationOptions, callback func() error) error {
	if options.StatePath == "" {
		if callback != nil {
			return callback()
		}
		return nil
	}
	if options.DatabasePath == "" {
		options.DatabasePath = filepath.Join(filepath.Dir(options.StatePath), "veil.db")
	}
	return managementstate.WithSnapshotBarrier(options.StatePath, func() error {
		if err := recoverKeyRotationLocked(options); err != nil {
			return err
		}
		if callback != nil {
			return callback()
		}
		return nil
	})
}

func recoverKeyRotationLocked(options RecoverKeyRotationOptions) error {
	journal, ok, err := loadKeyRotationJournal(options.StatePath)
	if err != nil || !ok {
		return err
	}
	db, err := storage.OpenExisting(options.DatabasePath)
	if err != nil {
		return fmt.Errorf("state commit: pending key rotation requires database: %w", err)
	}
	defer db.Close()
	revisions, err := apply.NewRevisionStore(db).Get()
	if err != nil {
		return fmt.Errorf("state commit: read revisions for key recovery: %w", err)
	}
	sourceDigest, sourceExists, err := currentRotationDigest(journal.SourceKeyPath)
	if err != nil {
		return err
	}
	keyDigest, keyExists, err := currentRotationDigest(journal.LiveKeyPath)
	if err != nil {
		return err
	}
	stateDigest, stateExists, err := currentRotationDigest(journal.LiveStatePath)
	if err != nil {
		return err
	}
	sameKeyPath := filepath.Clean(journal.SourceKeyPath) == filepath.Clean(journal.LiveKeyPath)
	sourceKnown := sourceExists && sourceDigest == journal.PreviousKeySHA256
	keyKnown := sourceKnown
	if sameKeyPath {
		sourceKnown = sourceExists && (sourceDigest == journal.PreviousKeySHA256 || sourceDigest == journal.IntendedKeySHA256)
		keyKnown = sourceKnown
	} else {
		keyKnown = (journal.PreviousTargetKeyExists && keyExists && keyDigest == journal.PreviousTargetKeySHA256) ||
			(!journal.PreviousTargetKeyExists && !keyExists) || (keyExists && keyDigest == journal.IntendedKeySHA256)
	}
	stateKnown := stateExists && (stateDigest == journal.PreviousStateSHA256 || stateDigest == journal.IntendedStateSHA256)
	if !sourceKnown || !keyKnown || !stateKnown {
		return errors.New("state commit: key-rotation files match neither known transaction side")
	}

	switch revisions.Desired {
	case journal.PreviousRevision:
		if apply.NewSnapshotStore(db).Has(journal.IntendedRevision) {
			return fmt.Errorf("state commit: rotation revision %d has a snapshot but is not desired", journal.IntendedRevision)
		}
		return rollbackKeyRotation(journal)
	case journal.IntendedRevision:
		if !keyExists || keyDigest != journal.IntendedKeySHA256 || stateDigest != journal.IntendedStateSHA256 {
			return errors.New("state commit: committed key rotation does not have the intended key/state pair")
		}
		return finalizeKeyRotation(db, journal)
	default:
		return fmt.Errorf("state commit: key rotation expects desired revision %d or %d, database has %d", journal.PreviousRevision, journal.IntendedRevision, revisions.Desired)
	}
}

func commitRotationRevision(
	db *sql.DB,
	journal keyRotationJournal,
	snapshot model.ManagementSnapshot,
	oldCipher, newCipher *secrets.Cipher,
) (uint64, error) {
	tx, err := client.NewRepository(db).BeginTx()
	if err != nil {
		return 0, fmt.Errorf("state commit: begin key-rotation transaction: %w", err)
	}
	if err := tx.ReencryptCredentials(oldCipher, newCipher); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	payload, err := immutableSnapshotPayloadFromSource(tx, snapshot, newCipher)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	revision, err := apply.BumpDesiredTx(tx)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if revision != journal.IntendedRevision {
		_ = tx.Rollback()
		return 0, fmt.Errorf("state commit: key rotation advanced revision to %d, want %d", revision, journal.IntendedRevision)
	}
	if err := apply.SaveSnapshotTxBound(tx, revision, payload, journal.IntendedStateSHA256); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return revision, &uncertainRotationCommitError{err: fmt.Errorf("state commit: commit key-rotation revision %d: %w", revision, err)}
	}
	return revision, nil
}

func rollbackKeyRotation(journal keyRotationJournal) error {
	previousState, err := readAndVerifyRotationArtifact(journal.PreviousStatePath, journal.PreviousStateSHA256)
	if err != nil {
		return err
	}
	stateMeta := rotationFileMetadata{mode: os.FileMode(journal.PreviousStateMode), uid: journal.PreviousStateUID, gid: journal.PreviousStateGID}
	if err := writeRotationFile(journal.LiveStatePath, previousState, stateMeta); err != nil {
		return fmt.Errorf("restore previous state: %w", err)
	}
	previousKey, err := readAndVerifyRotationArtifact(journal.PreviousKeyPath, journal.PreviousKeySHA256)
	if err != nil {
		return err
	}
	keyMeta := rotationFileMetadata{mode: os.FileMode(journal.PreviousKeyMode), uid: journal.PreviousKeyUID, gid: journal.PreviousKeyGID}
	if err := writeRotationFile(journal.SourceKeyPath, previousKey, keyMeta); err != nil {
		return fmt.Errorf("restore previous source key: %w", err)
	}
	if filepath.Clean(journal.LiveKeyPath) != filepath.Clean(journal.SourceKeyPath) {
		if journal.PreviousTargetKeyExists {
			previousTarget, err := readAndVerifyRotationArtifact(journal.PreviousTargetKeyPath, journal.PreviousTargetKeySHA256)
			if err != nil {
				return err
			}
			targetMeta := rotationFileMetadata{mode: os.FileMode(journal.PreviousTargetKeyMode), uid: journal.PreviousTargetKeyUID, gid: journal.PreviousTargetKeyGID}
			if err := writeRotationFile(journal.LiveKeyPath, previousTarget, targetMeta); err != nil {
				return fmt.Errorf("restore previous target key: %w", err)
			}
		} else if err := removeRotationFile(journal.LiveKeyPath); err != nil {
			return fmt.Errorf("remove newly published target key: %w", err)
		}
	}
	sourceDigest, sourceExists, err := currentRotationDigest(journal.SourceKeyPath)
	if err != nil {
		return err
	}
	stateDigest, stateExists, err := currentRotationDigest(journal.LiveStatePath)
	if err != nil {
		return err
	}
	if !sourceExists || sourceDigest != journal.PreviousKeySHA256 || !stateExists || stateDigest != journal.PreviousStateSHA256 {
		return errors.New("state commit: restored source key/state pair does not match journal")
	}
	if filepath.Clean(journal.LiveKeyPath) != filepath.Clean(journal.SourceKeyPath) {
		targetDigest, targetExists, err := currentRotationDigest(journal.LiveKeyPath)
		if err != nil {
			return err
		}
		if targetExists != journal.PreviousTargetKeyExists || targetDigest != journal.PreviousTargetKeySHA256 {
			return errors.New("state commit: restored target key does not match journal")
		}
	}
	return cleanupKeyRotation(journal)
}

func finalizeKeyRotation(db *sql.DB, journal keyRotationJournal) error {
	sourceDigest, sourceExists, err := currentRotationDigest(journal.SourceKeyPath)
	if err != nil {
		return err
	}
	keyDigest, keyExists, err := currentRotationDigest(journal.LiveKeyPath)
	if err != nil {
		return err
	}
	stateDigest, stateExists, err := currentRotationDigest(journal.LiveStatePath)
	if err != nil {
		return err
	}
	wantSourceDigest := journal.PreviousKeySHA256
	if filepath.Clean(journal.SourceKeyPath) == filepath.Clean(journal.LiveKeyPath) {
		wantSourceDigest = journal.IntendedKeySHA256
	}
	if !sourceExists || sourceDigest != wantSourceDigest {
		return errors.New("state commit: final source key does not match intended transaction side")
	}
	if !keyExists || keyDigest != journal.IntendedKeySHA256 || !stateExists || stateDigest != journal.IntendedStateSHA256 {
		return errors.New("state commit: final key/state pair does not match intended digests")
	}
	var snapshotDigest string
	if err := db.QueryRow(`SELECT state_sha256 FROM revision_snapshots WHERE revision=?`, journal.IntendedRevision).Scan(&snapshotDigest); err != nil {
		return fmt.Errorf("state commit: read committed rotation snapshot: %w", err)
	}
	if snapshotDigest != journal.IntendedStateSHA256 {
		return fmt.Errorf("state commit: rotation snapshot digest=%s want=%s", snapshotDigest, journal.IntendedStateSHA256)
	}
	return cleanupKeyRotation(journal)
}

func cleanupKeyRotation(journal keyRotationJournal) error {
	// The rollback/finalize caller has already verified the live pair and SQLite
	// decision. Remove the decision record first; a crash after this point can
	// leave only harmless safety-file orphans, never an unrecoverable journal
	// whose required bytes were deleted underneath it.
	if err := removeRotationFile(KeyRotationJournalPath(journal.LiveStatePath)); err != nil {
		return err
	}
	var cleanupErr error
	for _, path := range []string{journal.IntendedKeyPath, journal.IntendedStatePath} {
		if err := removeRotationFile(path); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	if !journal.KeepSafetyCopy {
		for _, path := range []string{journal.PreviousKeyPath, journal.PreviousTargetKeyPath, journal.PreviousStatePath} {
			if path == "" {
				continue
			}
			if err := removeRotationFile(path); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
	}
	return cleanupErr
}

func writeKeyRotationJournal(journal keyRotationJournal) error {
	body, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	return writeRotationFile(KeyRotationJournalPath(journal.LiveStatePath), append(body, '\n'), rotationFileMetadata{mode: 0o600, uid: -1, gid: -1})
}

func loadKeyRotationJournal(statePath string) (keyRotationJournal, bool, error) {
	var journal keyRotationJournal
	body, err := os.ReadFile(KeyRotationJournalPath(statePath))
	if errors.Is(err, os.ErrNotExist) {
		return journal, false, nil
	}
	if err != nil {
		return journal, false, fmt.Errorf("state commit: read key-rotation journal: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return journal, false, fmt.Errorf("state commit: decode key-rotation journal: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected data after journal")
		}
		return journal, false, fmt.Errorf("state commit: decode key-rotation journal: %w", err)
	}
	if err := validateKeyRotationJournal(journal, statePath); err != nil {
		return journal, false, err
	}
	return journal, true, nil
}

func validateKeyRotationJournal(journal keyRotationJournal, statePath string) error {
	if journal.Version != keyRotationJournalVersion {
		return fmt.Errorf("state commit: unsupported key-rotation journal version %d", journal.Version)
	}
	switch journal.Phase {
	case rotationPhasePrepared, rotationPhaseKeyPublished, rotationPhaseStatePublished, rotationPhaseSQLiteCommitted:
	default:
		return fmt.Errorf("state commit: unknown key-rotation phase %q", journal.Phase)
	}
	if journal.LiveStatePath != statePath || journal.SourceKeyPath == "" || journal.LiveKeyPath == "" {
		return errors.New("state commit: key-rotation journal live paths are invalid")
	}
	if journal.PreviousRevision == math.MaxUint64 || journal.IntendedRevision != journal.PreviousRevision+1 {
		return errors.New("state commit: key-rotation journal revision boundary is invalid")
	}
	for name, digest := range map[string]string{
		"source key": journal.SourceKeySHA256, "intended key": journal.IntendedKeySHA256,
		"previous state": journal.PreviousStateSHA256, "intended state": journal.IntendedStateSHA256,
	} {
		if !validDigest(digest) {
			return fmt.Errorf("state commit: key-rotation %s digest is invalid", name)
		}
	}
	if !journal.PreviousKeyExists || !validDigest(journal.PreviousKeySHA256) || journal.SourceKeySHA256 != journal.PreviousKeySHA256 {
		return errors.New("state commit: key-rotation previous source-key digest is inconsistent")
	}
	if journal.PreviousTargetKeyExists != validDigest(journal.PreviousTargetKeySHA256) {
		return errors.New("state commit: key-rotation previous-target-key digest is inconsistent")
	}
	if journal.PreviousStatePath == "" || journal.IntendedStatePath != journal.LiveStatePath+keyRotationIntendedSuffix ||
		journal.IntendedKeyPath != journal.LiveKeyPath+keyRotationIntendedSuffix {
		return errors.New("state commit: key-rotation safety paths are invalid")
	}
	if !strings.HasPrefix(filepath.Clean(journal.PreviousStatePath), filepath.Clean(journal.LiveStatePath)+".pre-rotation-") ||
		!strings.HasPrefix(filepath.Clean(journal.PreviousKeyPath), filepath.Clean(journal.SourceKeyPath)+".pre-rotation-") ||
		!strings.HasPrefix(filepath.Clean(journal.PreviousTargetKeyPath), filepath.Clean(journal.LiveKeyPath)+".pre-rotation-target-") {
		return errors.New("state commit: key-rotation previous-file paths are invalid")
	}
	return nil
}

func readAndVerifyRotationArtifact(path, wantDigest string) ([]byte, error) {
	body, _, err := readRotationFile(path, 0)
	if err != nil {
		return nil, err
	}
	if got := digestBytes(body); got != wantDigest {
		return nil, fmt.Errorf("state commit: rotation artifact %s digest=%s want=%s", path, got, wantDigest)
	}
	return body, nil
}

func readOptionalRotationFile(path string, exactSize int64) ([]byte, bool, rotationFileMetadata, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, rotationFileMetadata{uid: -1, gid: -1}, nil
	}
	if err != nil {
		return nil, false, rotationFileMetadata{}, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, rotationFileMetadata{}, fmt.Errorf("%s is not a regular file", path)
	}
	if exactSize > 0 && info.Size() != exactSize {
		return nil, false, rotationFileMetadata{}, fmt.Errorf("%s has size %d, want %d", path, info.Size(), exactSize)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false, rotationFileMetadata{}, err
	}
	return body, true, fileMetadata(info), nil
}

func readRotationFile(path string, exactSize int64) ([]byte, rotationFileMetadata, error) {
	body, ok, metadata, err := readOptionalRotationFile(path, exactSize)
	if err != nil {
		return nil, metadata, err
	}
	if !ok {
		return nil, metadata, os.ErrNotExist
	}
	return body, metadata, nil
}

func writeRotationFile(path string, body []byte, metadata rotationFileMetadata) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		return err
	}
	mode := metadata.mode.Perm()
	if mode == 0 {
		mode = 0o600
	}
	if err := tmp.Chmod(mode); err != nil {
		return err
	}
	if err := applyFileOwnership(tmp, metadata.uid, metadata.gid); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	syncRotationDirectory(filepath.Dir(path))
	return nil
}

func removeRotationFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	syncRotationDirectory(filepath.Dir(path))
	return nil
}

func syncRotationDirectory(path string) {
	if runtime.GOOS == "windows" {
		return
	}
	dir, err := os.Open(path)
	if err != nil {
		return
	}
	defer dir.Close()
	_ = dir.Sync()
}

func cipherForKey(body []byte) (*secrets.Cipher, error) {
	if len(body) != secrets.KeySize {
		return nil, fmt.Errorf("state commit: key has wrong length: %d", len(body))
	}
	var key [secrets.KeySize]byte
	copy(key[:], body)
	return secrets.NewCipher(key)
}

func currentRotationDigest(path string) (string, bool, error) {
	body, ok, _, err := readOptionalRotationFile(path, 0)
	if err != nil || !ok {
		return "", ok, err
	}
	return digestBytes(body), true, nil
}

func digestBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func digestIfPresent(body []byte, exists bool) string {
	if !exists {
		return ""
	}
	return digestBytes(body)
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
