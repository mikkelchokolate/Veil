package statecommit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

// UpdateOptions identifies the authoritative key/state/SQLite stores for an
// out-of-process read-modify-write. KeyPath is read only after the snapshot
// barrier is held, so a concurrent key rotation cannot leave the caller using
// a stale cipher.
type UpdateOptions struct {
	StatePath    string
	KeyPath      string
	DatabasePath string
	AllowCreate  bool

	// beforeBarrier is a package-private deterministic concurrency seam.
	beforeBarrier func()
}

// Update serializes the authoritative state/key/revision read, caller mutation,
// state publication and immutable revision commit under one snapshot barrier.
// The callback must only mutate the supplied current snapshot; it must not call
// Save, Update or another barrier-taking operation.
func Update(options UpdateOptions, mutate func(*model.ManagementSnapshot) error) (uint64, error) {
	if options.StatePath == "" || options.KeyPath == "" {
		return 0, errors.New("state commit: state and key paths are required for update")
	}
	if mutate == nil {
		return 0, errors.New("state commit: update callback is required")
	}
	if options.DatabasePath == "" {
		options.DatabasePath = filepath.Join(filepath.Dir(options.StatePath), "veil.db")
	}
	var revision uint64
	if options.beforeBarrier != nil {
		options.beforeBarrier()
	}
	err := managementstate.WithSnapshotBarrier(options.StatePath, func() error {
		if err := recoverKeyRotationLocked(RecoverKeyRotationOptions{
			StatePath: options.StatePath, DatabasePath: options.DatabasePath,
		}); err != nil {
			return fmt.Errorf("state commit: recover key rotation before update: %w", err)
		}
		var cipher *secrets.Cipher
		if options.AllowCreate {
			key, err := secrets.LoadOrCreateKey(options.KeyPath)
			if err != nil {
				return fmt.Errorf("state commit: load or create update key: %w", err)
			}
			cipher, err = secrets.NewCipher(*key)
			if err != nil {
				return err
			}
		} else {
			keyBody, _, err := readRotationFile(options.KeyPath, secrets.KeySize)
			if err != nil {
				return fmt.Errorf("state commit: read update key: %w", err)
			}
			cipher, err = cipherForKey(keyBody)
			if err != nil {
				return err
			}
		}
		store := managementstate.NewStore(options.StatePath, cipher)
		snapshot, ok, err := store.Load()
		if err != nil {
			return fmt.Errorf("state commit: load current state for update: %w", err)
		}
		if !ok && !options.AllowCreate {
			return fmt.Errorf("state commit: no state found at %s", options.StatePath)
		}
		if err := mutate(&snapshot); err != nil {
			return err
		}
		if _, err := os.Stat(options.DatabasePath); errors.Is(err, os.ErrNotExist) {
			return store.Save(snapshot)
		} else if err != nil {
			return fmt.Errorf("state commit: stat update database: %w", err)
		}
		db, err := storage.OpenExisting(options.DatabasePath)
		if err != nil {
			return err
		}
		defer db.Close()
		revision, err = saveWithDBLocked(db, snapshot, Options{
			StatePath: options.StatePath, DatabasePath: options.DatabasePath, Cipher: cipher,
		})
		return err
	})
	return revision, err
}
