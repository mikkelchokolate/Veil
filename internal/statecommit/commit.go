// Package statecommit coordinates out-of-process management-state writers with
// Veil's SQLite desired revision and immutable snapshot stores.
package statecommit

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

// Options identifies the two durable stores participating in a state commit.
type Options struct {
	StatePath    string
	DatabasePath string
	Cipher       *secrets.Cipher
}

// Save publishes snapshot and, when veil.db already exists, commits a matching
// desired revision plus immutable snapshot under the cross-process barrier.
// The no-database path is intentionally retained for first-install/bootstrap
// writers; the panel creates and tracks SQLite before accepting API mutations.
func Save(snapshot model.ManagementSnapshot, options Options) (uint64, error) {
	if options.StatePath == "" {
		return 0, errors.New("state commit: state path is required")
	}
	store := managementstate.NewStore(options.StatePath, options.Cipher)
	databasePath := options.DatabasePath
	if databasePath == "" {
		databasePath = filepath.Join(filepath.Dir(options.StatePath), "veil.db")
	}
	if _, err := os.Stat(databasePath); errors.Is(err, os.ErrNotExist) {
		return 0, store.Save(snapshot)
	} else if err != nil {
		return 0, fmt.Errorf("state commit: stat database: %w", err)
	}

	var committedRevision uint64
	err := managementstate.WithSnapshotBarrier(options.StatePath, func() error {
		db, err := storage.Open(databasePath)
		if err != nil {
			return err
		}
		defer db.Close()
		revisions, err := apply.NewRevisionStore(db).Get()
		if err != nil {
			return fmt.Errorf("state commit: read revisions: %w", err)
		}
		encoded, err := store.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("state commit: encode state: %w", err)
		}
		payload, err := immutableSnapshotPayload(db, snapshot, options.Cipher)
		if err != nil {
			return err
		}
		commit, err := store.PrepareStateCommit(encoded, revisions.Desired, revisions.Desired+1)
		if err != nil {
			return err
		}
		rollbackState := func(commitErr error) error {
			if rollbackErr := commit.Rollback(); rollbackErr != nil {
				return errors.Join(commitErr, fmt.Errorf("state commit: restore previous state: %w", rollbackErr))
			}
			return commitErr
		}

		tx, err := db.Begin()
		if err != nil {
			return rollbackState(fmt.Errorf("state commit: begin SQLite transaction: %w", err))
		}
		revision, err := apply.BumpDesiredTx(tx)
		if err != nil {
			_ = tx.Rollback()
			return rollbackState(err)
		}
		if revision != revisions.Desired+1 {
			_ = tx.Rollback()
			return rollbackState(fmt.Errorf("state commit: revision advanced to %d, want %d", revision, revisions.Desired+1))
		}
		stateDigest := commit.Journal().IntendedStateSHA256
		if err := apply.SaveSnapshotTxBound(tx, revision, payload, stateDigest); err != nil {
			_ = tx.Rollback()
			return rollbackState(err)
		}
		if err := tx.Commit(); err != nil {
			return rollbackState(fmt.Errorf("state commit: commit revision %d: %w", revision, err))
		}
		committedRevision = revision
		// Both durable stores committed. Startup can clean a marker that could not
		// be removed, so a cleanup error must not produce a false mutation failure.
		if err := commit.Finalize(); err != nil {
			log.Printf("state commit: revision %d left recovery marker: %v", revision, err)
		}
		return nil
	})
	return committedRevision, err
}

func immutableSnapshotPayload(db *sql.DB, snapshot model.ManagementSnapshot, cipher *secrets.Cipher) ([]byte, error) {
	repository := client.NewRepository(db)
	clients, err := repository.AllClients()
	if err != nil {
		return nil, fmt.Errorf("state commit: snapshot clients: %w", err)
	}
	bindings, err := repository.AllBindings()
	if err != nil {
		return nil, fmt.Errorf("state commit: snapshot bindings: %w", err)
	}
	credentials, err := repository.AllActiveCredentials()
	if err != nil {
		return nil, fmt.Errorf("state commit: snapshot credentials: %w", err)
	}
	input := managementstate.SnapshotInput{
		Setup:         snapshot.Setup,
		Settings:      snapshot.Settings,
		Inbounds:      snapshot.Inbounds,
		Rules:         snapshot.Rules,
		RoutingPreset: snapshot.RoutingPreset,
		RoutingSource: snapshot.RoutingSource,
		Warp:          snapshot.Warp,
		Users:         snapshot.Users,
	}
	input.Clients, input.Bindings, input.Credentials = snapshotClientRows(clients, bindings, credentials)
	immutable := managementstate.BuildSnapshot(input)
	if err := managementstate.EncryptSnapshot(&immutable, cipher); err != nil {
		return nil, fmt.Errorf("state commit: encrypt immutable snapshot: %w", err)
	}
	payload, err := json.Marshal(immutable)
	if err != nil {
		return nil, fmt.Errorf("state commit: marshal immutable snapshot: %w", err)
	}
	return payload, nil
}

func snapshotClientRows(
	clients []client.Client, bindings []client.Binding, credentials []client.Credential,
) ([]model.ClientSnapshot, []model.BindingSnapshot, []model.CredentialSnapshot) {
	clientRows := make([]model.ClientSnapshot, 0, len(clients))
	for _, item := range clients {
		clientRows = append(clientRows, model.ClientSnapshot{
			ID: item.ID, Name: item.Name, Email: item.Email, Enabled: item.Enabled,
			GroupID: item.GroupID, QuotaBytes: item.QuotaBytes, QuotaResetPolicy: item.QuotaResetPolicy,
			QuotaResetAt: item.QuotaResetAt, ExpiresAt: item.ExpiresAt, DeviceLimit: item.DeviceLimit,
			Depleted: item.Depleted, Version: item.Version,
		})
	}
	bindingRows := make([]model.BindingSnapshot, 0, len(bindings))
	for _, item := range bindings {
		bindingRows = append(bindingRows, model.BindingSnapshot{
			ID: item.ID, ClientID: item.ClientID, InboundID: item.InboundID, Enabled: item.Enabled,
			ProtocolSettings: item.ProtocolSettings, Version: item.Version,
		})
	}
	credentialRows := make([]model.CredentialSnapshot, 0, len(credentials))
	for _, item := range credentials {
		credentialRows = append(credentialRows, model.CredentialSnapshot{
			ID: item.ID, BindingID: item.BindingID, Kind: item.Kind, EncryptedValue: item.EncryptedValue,
			KeyVersion: item.KeyVersion, CredentialVersion: item.CredentialVersion,
		})
	}
	return clientRows, bindingRows, credentialRows
}
