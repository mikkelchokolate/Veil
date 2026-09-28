package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
)

type applyRollbackRequest struct {
	SelectedRevision uint64 `json:"selectedRevision"`
	Confirm          bool   `json:"confirm"`
}

var rollbackRuntimeIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,48}$`)

func (s *managementState) handleApplyRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var request applyRollbackRequest
	if !decodeJSONRequest(w, r, &request) {
		return
	}
	if !request.Confirm {
		writeError(w, "rollback requires explicit confirmation", http.StatusBadRequest)
		return
	}
	if request.SelectedRevision == 0 {
		writeError(w, "selectedRevision must be greater than zero", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.applyTrackingEnabled() || s.applySnapshots == nil || s.clientRepo == nil {
		writeError(w, "durable apply tracking is unavailable", http.StatusServiceUnavailable)
		return
	}
	current, err := s.applyRevisions.Get()
	if err != nil {
		writeError(w, "failed to read apply revisions", http.StatusServiceUnavailable)
		return
	}
	if request.SelectedRevision >= current.Desired {
		writeError(w, "selectedRevision must identify an older immutable revision", http.StatusConflict)
		return
	}
	payload, err := s.applySnapshots.Load(request.SelectedRevision)
	if err != nil {
		writeError(w, "selected immutable revision was not found", http.StatusNotFound)
		return
	}
	var selected managementSnapshot
	if err := json.Unmarshal(payload, &selected); err != nil {
		writeError(w, "selected immutable revision is corrupt", http.StatusInternalServerError)
		return
	}
	if err := s.decryptSnapshot(&selected); err != nil {
		writeError(w, "selected immutable revision cannot be decrypted", http.StatusInternalServerError)
		return
	}
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(payload, &fields)
	if validationErrors := NewManagementStateValidation().ValidateSnapshot(selected, fields); len(validationErrors) > 0 {
		writeError(w, "selected immutable revision is invalid: "+validationErrors[0], http.StatusUnprocessableEntity)
		return
	}
	if err := validateRollbackNormalizedSnapshot(selected); err != nil {
		writeError(w, "selected immutable revision is invalid: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	actor := actorFromRequest(r)
	newRevision, err := s.commitIntentionalRollbackLocked(request.SelectedRevision, payload, selected, actor)
	if err != nil {
		writeError(w, "rollback commit failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	applyManagementSnapshotExact(s, selected)
	s.registerTrafficProvidersLocked()
	outcome := s.autoApplyResultLocked(r, actor)
	response := map[string]any{
		"selectedRevision": request.SelectedRevision,
		"desiredRevision":  newRevision,
		"success":          outcome.success,
	}
	s.mergeOutcomeInto(response, outcome)
	_ = s.auditRecorder().Append(audit.Record{
		Actor: actor, Role: roleFromRequestContext(r), Action: "apply.rollback",
		Target:  fmt.Sprintf("revision:%d->%d", request.SelectedRevision, newRevision),
		Success: outcome.success,
	})
	writeJSON(w, response)
}

func (s *managementState) commitIntentionalRollbackLocked(selectedRevision uint64, payload []byte, selected managementSnapshot, actor string) (uint64, error) {
	var newRevision uint64
	err := managementstate.WithSnapshotBarrier(s.statePath, func() error {
		current, err := s.applyRevisions.Get()
		if err != nil {
			return err
		}
		store := managementstate.NewStore(s.statePath, s.cipher)
		// Authentication and normalized-client security state are monotonic:
		// a rollback restores configuration only. Graft the live panel
		// users/setup (#1094) and merge the live client, binding, and
		// credential security posture (#1099) onto the selected snapshot so
		// the committed state file, the stored revision payload, and every
		// render of the new revision carry the current revocations forward.
		selected.Setup = s.setup
		selected.Users = append([]User(nil), s.users...)

		tx, err := s.clientRepo.BeginTx()
		if err != nil {
			return err
		}
		if err := graftRollbackClientSecurityTx(tx, &selected); err != nil {
			_ = tx.Rollback()
			return err
		}
		encodedState, err := store.Marshal(selected)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("encode selected state: %w", err)
		}
		stateCommit, err := store.PrepareStateCommit(encodedState, current.Desired, current.Desired+1)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		rollbackState := func(cause error) error {
			if rollbackErr := stateCommit.Rollback(); rollbackErr != nil {
				return fmt.Errorf("%v; restore previous state: %w", cause, rollbackErr)
			}
			return cause
		}

		// The immutable payload recorded for the new revision must carry the
		// grafted security state — persisting the historical bytes verbatim
		// would let a later render of this revision resurrect revoked
		// credentials or deleted accounts.
		stored := managementstate.BuildSnapshot(managementstate.SnapshotInput{
			EffectiveAt: selected.EffectiveAt, Setup: selected.Setup, Settings: selected.Settings,
			Inbounds: selected.Inbounds, Rules: selected.Rules, RoutingPreset: selected.RoutingPreset,
			RoutingSource: selected.RoutingSource, Warp: selected.Warp, Users: selected.Users,
			Clients: selected.Clients, Bindings: selected.Bindings, Credentials: selected.Credentials,
		})
		if err := s.encryptSnapshot(&stored); err != nil {
			_ = tx.Rollback()
			return rollbackState(fmt.Errorf("encrypt selected snapshot: %w", err))
		}
		snapshotPayload, err := json.Marshal(stored)
		if err != nil {
			_ = tx.Rollback()
			return rollbackState(fmt.Errorf("encode selected snapshot: %w", err))
		}
		clients, bindings, credentials := clientRowsFromImmutableSnapshot(selected)
		if err := client.ReplaceSnapshotTx(tx, clients, bindings, credentials); err != nil {
			_ = tx.Rollback()
			return rollbackState(err)
		}
		newRevision, err = apply.BumpDesiredTx(tx)
		if err != nil {
			_ = tx.Rollback()
			return rollbackState(err)
		}
		if newRevision != current.Desired+1 {
			_ = tx.Rollback()
			return rollbackState(fmt.Errorf("desired revision advanced to %d, want %d", newRevision, current.Desired+1))
		}
		if err := apply.SaveSnapshotTxBound(tx, newRevision, snapshotPayload, stateCommit.Journal().IntendedStateSHA256); err != nil {
			_ = tx.Rollback()
			return rollbackState(err)
		}
		digest := sha256.Sum256(payload)
		if _, err := tx.Exec(`INSERT INTO apply_rollbacks
  (id, selected_revision, new_revision, actor_id, selected_snapshot_sha256, created_at)
  VALUES(?,?,?,?,?,?)`, uuid.NewString(), selectedRevision, newRevision, actor,
			hex.EncodeToString(digest[:]), time.Now().Unix()); err != nil {
			_ = tx.Rollback()
			return rollbackState(fmt.Errorf("persist immutable rollback audit: %w", err))
		}
		if err := tx.Commit(); err != nil {
			return rollbackState(err)
		}
		if err := stateCommit.Finalize(); err != nil {
			log.Printf("apply rollback revision %d left recovery marker: %v", newRevision, err)
		}
		return nil
	})
	return newRevision, err
}

func validateRollbackNormalizedSnapshot(snapshot managementSnapshot) error {
	clients := make(map[string]struct{}, len(snapshot.Clients))
	for _, current := range snapshot.Clients {
		if current.ID == "" {
			return fmt.Errorf("client id is empty")
		}
		if _, duplicate := clients[current.ID]; duplicate {
			return fmt.Errorf("duplicate client id %q", current.ID)
		}
		clients[current.ID] = struct{}{}
	}
	inbounds := make(map[string]struct{}, len(snapshot.Inbounds))
	for _, inbound := range snapshot.Inbounds {
		if inbound.Name == "" {
			return fmt.Errorf("inbound name is empty")
		}
		inbounds[inbound.Name] = struct{}{}
	}
	bindings := make(map[string]string, len(snapshot.Bindings))
	identities := make(map[string]string, len(snapshot.Bindings))
	for _, binding := range snapshot.Bindings {
		if binding.ID == "" {
			return fmt.Errorf("binding id is empty")
		}
		if _, duplicate := bindings[binding.ID]; duplicate {
			return fmt.Errorf("duplicate binding id %q", binding.ID)
		}
		if _, ok := clients[binding.ClientID]; !ok {
			return fmt.Errorf("binding %q references missing client %q", binding.ID, binding.ClientID)
		}
		if _, ok := inbounds[binding.InboundID]; !ok {
			return fmt.Errorf("binding %q references missing inbound %q", binding.ID, binding.InboundID)
		}
		if !rollbackRuntimeIdentityPattern.MatchString(binding.RuntimeIdentity) {
			return fmt.Errorf("binding %q has invalid runtime identity", binding.ID)
		}
		identityKey := binding.InboundID + "\x00" + binding.RuntimeIdentity
		if previous, duplicate := identities[identityKey]; duplicate {
			return fmt.Errorf("bindings %q and %q duplicate runtime identity for inbound %q", previous, binding.ID, binding.InboundID)
		}
		identities[identityKey] = binding.ID
		bindings[binding.ID] = binding.ClientID
	}
	credentials := make(map[string]struct{}, len(snapshot.Credentials))
	for _, credential := range snapshot.Credentials {
		if credential.ID == "" {
			return fmt.Errorf("credential id is empty")
		}
		if _, duplicate := credentials[credential.ID]; duplicate {
			return fmt.Errorf("duplicate credential id %q", credential.ID)
		}
		if _, ok := bindings[credential.BindingID]; !ok {
			return fmt.Errorf("credential %q references missing binding %q", credential.ID, credential.BindingID)
		}
		credentials[credential.ID] = struct{}{}
	}
	return nil
}

// graftRollbackClientSecurityTx merges the live normalized-client security
// posture into the selected snapshot inside the rollback transaction (#1099).
// Entities that were deleted after the snapshot are not resurrected; retained
// entities keep their configuration rollback but inherit the monotonic
// security bits — a disable, depletion, or expiry that happened after the
// snapshot is never undone, and a rotated runtime identity stays rotated.
// Credentials are replaced wholesale with the currently-active values for
// retained bindings so revoked or superseded material cannot return.
func graftRollbackClientSecurityTx(tx *client.Tx, selected *managementSnapshot) error {
	liveClients, err := tx.AllClients()
	if err != nil {
		return fmt.Errorf("read live clients for rollback graft: %w", err)
	}
	liveBindings, err := tx.AllBindings()
	if err != nil {
		return fmt.Errorf("read live bindings for rollback graft: %w", err)
	}
	liveCredentials, err := tx.AllActiveCredentials()
	if err != nil {
		return fmt.Errorf("read live credentials for rollback graft: %w", err)
	}

	liveClientByID := make(map[string]client.Client, len(liveClients))
	for _, live := range liveClients {
		liveClientByID[live.ID] = live
	}
	retainedClients := make(map[string]struct{}, len(selected.Clients))
	clients := make([]model.ClientSnapshot, 0, len(selected.Clients))
	for _, snapshotClient := range selected.Clients {
		live, ok := liveClientByID[snapshotClient.ID]
		if !ok {
			// Deleted after the snapshot — the deletion is a security event
			// and is never undone.
			continue
		}
		retainedClients[snapshotClient.ID] = struct{}{}
		snapshotClient.Enabled = snapshotClient.Enabled && live.Enabled
		snapshotClient.Depleted = snapshotClient.Depleted || live.Depleted
		snapshotClient.ExpiresAt = earlierExpiry(snapshotClient.ExpiresAt, live.ExpiresAt)
		clients = append(clients, snapshotClient)
	}
	selected.Clients = clients

	liveBindingByID := make(map[string]client.Binding, len(liveBindings))
	for _, live := range liveBindings {
		liveBindingByID[live.ID] = live
	}
	retainedBindings := make(map[string]struct{}, len(selected.Bindings))
	bindings := make([]model.BindingSnapshot, 0, len(selected.Bindings))
	for _, snapshotBinding := range selected.Bindings {
		live, ok := liveBindingByID[snapshotBinding.ID]
		if !ok {
			continue
		}
		if _, ok := retainedClients[live.ClientID]; !ok {
			// The binding's live parent client is not retained — the binding
			// would dangle after that client's deletion.
			continue
		}
		if _, ok := retainedClients[snapshotBinding.ClientID]; !ok {
			// The snapshot parent is gone; keep the binding on its live
			// parent rather than resurrecting the deleted client.
			snapshotBinding.ClientID = live.ClientID
		}
		snapshotBinding.Enabled = snapshotBinding.Enabled && live.Enabled
		if live.RuntimeIdentity != "" {
			snapshotBinding.RuntimeIdentity = live.RuntimeIdentity
		}
		bindings = append(bindings, snapshotBinding)
		retainedBindings[snapshotBinding.ID] = struct{}{}
	}
	selected.Bindings = bindings

	credentials := make([]model.CredentialSnapshot, 0, len(liveCredentials))
	for _, live := range liveCredentials {
		if _, ok := retainedBindings[live.BindingID]; !ok {
			continue
		}
		credentials = append(credentials, model.CredentialSnapshot{
			ID: live.ID, BindingID: live.BindingID, Kind: live.Kind,
			EncryptedValue:    append([]byte(nil), live.EncryptedValue...),
			KeyVersion:        live.KeyVersion,
			CredentialVersion: live.CredentialVersion,
			CreatedAt:         live.CreatedAt,
			RotatedAt:         live.RotatedAt,
		})
	}
	selected.Credentials = credentials
	return nil
}

// earlierExpiry returns the earlier non-nil expiry of a snapshot value and a
// live value. A rollback may shorten or restore an expiry, but it must never
// extend past — or remove — an expiry set after the snapshot was taken
// (#1099).
func earlierExpiry(snapshot, live *int64) *int64 {
	if snapshot == nil {
		return live
	}
	if live == nil {
		return snapshot
	}
	if *live < *snapshot {
		return live
	}
	return snapshot
}

func clientRowsFromImmutableSnapshot(snapshot managementSnapshot) ([]client.Client, []client.Binding, []client.Credential) {
	clients := make([]client.Client, 0, len(snapshot.Clients))
	for _, item := range snapshot.Clients {
		clients = append(clients, client.Client{
			ID: item.ID, Name: item.Name, Email: item.Email, Enabled: item.Enabled,
			GroupID: item.GroupID, QuotaBytes: item.QuotaBytes, QuotaResetPolicy: item.QuotaResetPolicy,
			QuotaResetAt: item.QuotaResetAt, ExpiresAt: item.ExpiresAt, DeviceLimit: item.DeviceLimit,
			Notes: item.Notes, Depleted: item.Depleted, CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt, Version: item.Version,
		})
	}
	bindings := make([]client.Binding, 0, len(snapshot.Bindings))
	for _, item := range snapshot.Bindings {
		bindings = append(bindings, client.Binding{
			ID: item.ID, ClientID: item.ClientID, InboundID: item.InboundID,
			RuntimeIdentity: item.RuntimeIdentity,
			Enabled:         item.Enabled, ProtocolSettings: item.ProtocolSettings,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, Version: item.Version,
		})
	}
	credentials := make([]client.Credential, 0, len(snapshot.Credentials))
	for _, item := range snapshot.Credentials {
		credentials = append(credentials, client.Credential{
			ID: item.ID, BindingID: item.BindingID, Kind: item.Kind,
			EncryptedValue: append([]byte(nil), item.EncryptedValue...), KeyVersion: item.KeyVersion,
			CredentialVersion: item.CredentialVersion, CreatedAt: item.CreatedAt,
			RotatedAt: item.RotatedAt,
		})
	}
	return clients, bindings, credentials
}

func applyManagementSnapshotExact(state *managementState, snapshot managementSnapshot) {
	cloned := managementstate.BuildSnapshot(managementstate.SnapshotInput{
		Settings: snapshot.Settings, Inbounds: snapshot.Inbounds,
		Rules: snapshot.Rules, RoutingPreset: snapshot.RoutingPreset,
		RoutingSource: snapshot.RoutingSource, Warp: snapshot.Warp,
	})
	// Panel authentication state is not configuration: the current users and
	// setup flags survive the rollback so a historical snapshot can never
	// resurrect a deleted account, roll a password hash backwards, demote an
	// administrator, or un-complete setup (#1094).
	state.settings = cloned.Settings
	state.inbounds = cloned.Inbounds
	state.rules = cloned.Rules
	state.routingPreset = cloned.RoutingPreset
	state.routingSource = cloned.RoutingSource
	state.warp = cloned.Warp
}

func roleFromRequestContext(r *http.Request) string {
	role, _ := r.Context().Value(contextKeyRole).(string)
	return role
}
