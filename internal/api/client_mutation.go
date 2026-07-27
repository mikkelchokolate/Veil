package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// client_mutation.go is the single mutation orchestration for the normalized
// client domain (blocker A1): Client/Binding/Credential writes, the desired-
// revision bump, and the immutable snapshot all commit in ONE SQLite
// transaction. Before this, handlers committed the client tables first and
// bumped the revision afterwards — a snapshot/revision failure left a
// committed mutation with no pinned revision, and errors were only logged.
// Now any failure rolls everything back and fails the API mutation honestly.

// withClientMutation runs mutate inside the atomic client+revision+snapshot
// transaction and then runs exactly one apply job for the committed revision.
// It returns the apply outcome for the honest mutation envelope, or an error
// when the mutation itself failed (nothing committed, nothing applied).
// Caller must NOT hold s.mu.
func (s *managementState) withClientMutation(r *http.Request, actor string, mutate func(tx *client.Tx) error) (autoApplyOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.commitClientMutationLocked(mutate)
	if errors.Is(err, errNoClientChanges) {
		// The closure determined nothing actually changed: transaction rolled
		// back, no revision, no apply. The response reports success against
		// the CURRENT revision state.
		return autoApplyOutcome{}, nil
	}
	if err != nil {
		return autoApplyOutcome{}, err
	}
	return s.autoApplyResultLocked(r, actor), nil
}

// commitClientMutationLocked runs a client-domain mutation, the desired-
// revision bump, and the immutable-snapshot write in ONE SQLite transaction.
// Caller must hold s.mu. On ANY error the whole transaction rolls back: no
// client rows, no revision, no snapshot — the mutation fails honestly.
// Returns the committed desired revision (0 when revision tracking is
// disabled, e.g. tests without a StatePath).
func (s *managementState) commitClientMutationLocked(mutate func(tx *client.Tx) error) (uint64, error) {
	if s.clientRepo == nil {
		return 0, fmt.Errorf("client store unavailable")
	}
	if !s.applyTrackingEnabled() {
		// Degraded path (no StatePath → no SQLite store): the mutation still
		// commits atomically on its own; revision/apply tracking is reported
		// as legacy mode by the response envelope.
		return 0, s.clientRepo.WithTx(mutate)
	}
	var revision uint64
	err := managementstate.WithSnapshotBarrier(s.statePath, func() error {
		stateDigest := ""
		if s.statePath != "" {
			var err error
			stateDigest, err = stateFileDigest(s.statePath)
			if err != nil {
				return fmt.Errorf("digest management state: %w", err)
			}
		}
		tx, err := s.clientRepo.BeginTx()
		if err != nil {
			return err
		}
		if err := mutate(tx); err != nil {
			_ = tx.Rollback()
			return err
		}
		revision, err = s.recordRevisionInTxLocked(tx, stateDigest)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return nil
	})
	return revision, err
}

// recordRevisionInTxLocked bumps the desired revision and writes the immutable
// snapshot inside an open client transaction. The snapshot reads the client
// tables through the SAME transaction, so it captures exactly the state being
// committed — never a pre-mutation snapshot or a mix of old and new rows.
// A revision or snapshot failure aborts the caller's mutation.
func (s *managementState) recordRevisionInTxLocked(tx *client.Tx, stateDigest string) (uint64, error) {
	clients, err := tx.AllClients()
	if err != nil {
		return 0, fmt.Errorf("snapshot: read clients: %w", err)
	}
	bindings, err := tx.AllBindings()
	if err != nil {
		return 0, fmt.Errorf("snapshot: read bindings: %w", err)
	}
	creds, err := tx.AllActiveCredentials()
	if err != nil {
		return 0, fmt.Errorf("snapshot: read credentials: %w", err)
	}
	snap := s.snapshotWithClientStateLocked(clients, bindings, creds)
	if err := s.encryptSnapshot(&snap); err != nil {
		return 0, fmt.Errorf("encrypt revision snapshot: %w", err)
	}
	payload, err := json.Marshal(snap)
	if err != nil {
		return 0, fmt.Errorf("marshal revision snapshot: %w", err)
	}
	rev, err := apply.BumpDesiredTx(tx)
	if err != nil {
		return 0, err
	}
	if err := apply.SaveSnapshotTxBound(tx, rev, payload, stateDigest); err != nil {
		return 0, err
	}
	return rev, nil
}

// snapshotWithClientStateLocked builds the immutable management snapshot from
// the in-memory management state plus the given normalized client rows
// (read inside the committing transaction). Caller must hold s.mu.
func (s *managementState) snapshotWithClientStateLocked(
	clients []client.Client, bindings []client.Binding, creds []client.Credential,
) managementSnapshot {
	input := managementstate.SnapshotInput{
		Setup:         s.setup,
		Settings:      s.settings,
		Inbounds:      s.inbounds,
		Rules:         s.rules,
		RoutingPreset: s.routingPreset,
		RoutingSource: s.routingSource,
		Warp:          s.warp,
		Users:         s.users,
	}
	input.Clients, input.Bindings, input.Credentials = clientSnapshotRows(clients, bindings, creds)
	return managementstate.BuildSnapshot(input)
}

// clientSnapshotRows converts repository rows into the snapshot model. Shared
// by the transactional mutation path and the state-file SnapshotLocked path.
func clientSnapshotRows(
	clients []client.Client, bindings []client.Binding, creds []client.Credential,
) ([]model.ClientSnapshot, []model.BindingSnapshot, []model.CredentialSnapshot) {
	cs := make([]model.ClientSnapshot, 0, len(clients))
	for _, c := range clients {
		cs = append(cs, model.ClientSnapshot{
			ID: c.ID, Name: c.Name, Email: c.Email, Enabled: c.Enabled,
			GroupID: c.GroupID, QuotaBytes: c.QuotaBytes, QuotaResetPolicy: c.QuotaResetPolicy,
			QuotaResetAt: c.QuotaResetAt, ExpiresAt: c.ExpiresAt, DeviceLimit: c.DeviceLimit,
			Notes: c.Notes, Depleted: c.Depleted, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Version: c.Version,
		})
	}
	bs := make([]model.BindingSnapshot, 0, len(bindings))
	for _, b := range bindings {
		bs = append(bs, model.BindingSnapshot{
			ID: b.ID, ClientID: b.ClientID, InboundID: b.InboundID, RuntimeIdentity: b.RuntimeIdentity, Enabled: b.Enabled,
			ProtocolSettings: b.ProtocolSettings, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt, Version: b.Version,
		})
	}
	ks := make([]model.CredentialSnapshot, 0, len(creds))
	for _, c := range creds {
		ks = append(ks, model.CredentialSnapshot{
			ID: c.ID, BindingID: c.BindingID, Kind: c.Kind, EncryptedValue: c.EncryptedValue,
			KeyVersion: c.KeyVersion, CredentialVersion: c.CredentialVersion,
			CreatedAt: c.CreatedAt, RotatedAt: c.RotatedAt,
		})
	}
	return cs, bs, ks
}
