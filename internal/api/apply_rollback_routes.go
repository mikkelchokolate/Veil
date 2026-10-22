package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// errRollbackSnapshotInvalid marks a rollback refused because the selected
// revision, once merged with the live security posture, would commit state
// the normal write paths reject. The handler reports it as 422, not as a
// commit failure (#1270).
var errRollbackSnapshotInvalid = errors.New("selected immutable revision is invalid")

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
	if errors.Is(err, errRollbackSnapshotInvalid) {
		writeError(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
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
		// The snapshot's own client values, captured before the graft
		// merges the live depletion/enablement/expiry posture onto them.
		snapshotClients, _, _ := clientRowsFromImmutableSnapshot(selected)
		if err := graftRollbackClientSecurityTx(tx, &selected); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := validateRollbackRestoredClients(snapshotClients, selected); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("%w: %v", errRollbackSnapshotInvalid, err)
		}
		// Checked after the graft so the verdict covers exactly the rows
		// ReplaceSnapshotTx is about to write: bindings deleted or disabled
		// live since the snapshot no longer count (#1270).
		if err := validateRollbackConnectionLimits(selected); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("%w: %v", errRollbackSnapshotInvalid, err)
		}
		// Same post-graft scope for the #1173 quota contract (#1351).
		if err := validateRollbackQuotaBindings(selected); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("%w: %v", errRollbackSnapshotInvalid, err)
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
		if err := apply.SaveSnapshotTxBound(tx, s.applySnapshots.Seal(), newRevision, snapshotPayload, stateCommit.Journal().IntendedStateSHA256); err != nil {
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

// validateRollbackRestoredClients runs the restored client rows through the
// same validate() rules as every live writer, so a legacy or crafted revision
// cannot restore deviceLimit=0, a negative or over-range quota, a
// non-positive expiry, an unknown reset policy, depletion without a quota or
// a malformed name that the API refuses (#1270).
//
// It checks the snapshot's own values (snapshotClients, captured before the
// graft) but only for the clients the graft retained (merged.Clients). A
// client deleted live since the snapshot is dropped by the graft (#1099) and
// never written, so a legacy value on it must not veto the rollback. Using
// the pre-graft values keeps the depletion clause scoped to the snapshot; the
// graft itself never carries a live depletion onto a quota-less client
// (#1350).
func validateRollbackRestoredClients(snapshotClients []client.Client, merged managementSnapshot) error {
	retained := make(map[string]struct{}, len(merged.Clients))
	for _, item := range merged.Clients {
		retained[item.ID] = struct{}{}
	}
	for _, restored := range snapshotClients {
		if _, ok := retained[restored.ID]; !ok {
			continue
		}
		if err := client.ValidateRestoredClient(restored); err != nil {
			return fmt.Errorf("client %q: %v", restored.ID, err)
		}
	}
	return nil
}

// validateRollbackConnectionLimits applies the #1224 contract that
// validateConnectionLimitBindingsLocked enforces on every live write to the
// state a rollback is about to commit: a client may carry deviceLimit/ipLimit
// only when each of its ENABLED bindings lands on an enabled inbound whose
// protocol enforces connection limits. The inbounds are the selected
// snapshot's, because the rollback restores them together with the clients.
// Otherwise the limits would commit silently inert while the API advertises
// them as enforced (#1270).
func validateRollbackConnectionLimits(snapshot managementSnapshot) error {
	capable := make(map[string]bool, len(snapshot.Inbounds))
	for _, inbound := range snapshot.Inbounds {
		capable[inbound.Name] = inbound.Enabled && protocolConnectionLimits(inbound.Protocol)
	}
	limited := make(map[string]bool, len(snapshot.Clients))
	for _, item := range snapshot.Clients {
		limited[item.ID] = item.DeviceLimit != nil || item.IPLimit != nil
	}
	for _, binding := range snapshot.Bindings {
		if !binding.Enabled || !limited[binding.ClientID] {
			continue
		}
		if !capable[binding.InboundID] {
			return fmt.Errorf("client %q: device/ip limits are unsupported for inbound %s", binding.ClientID, binding.InboundID)
		}
	}
	return nil
}

// validateRollbackQuotaBindings applies the #1173 contract that
// validateQuotaBindingsLocked enforces on every live write to the state a
// rollback is about to commit: a client may carry quotaBytes only when each
// of its ENABLED bindings lands on an enabled inbound whose protocol rejects
// a depleted client. As with validateRollbackConnectionLimits, the inbounds
// are the selected snapshot's and the bindings are the post-graft ones, so a
// binding disabled or deleted live since the snapshot does not count.
// Otherwise the quota would commit silently inert (the runtime never rejects
// the depleted client) while the API shows it as set, and the next quota
// edit on that client would be refused with 400 (#1351).
func validateRollbackQuotaBindings(snapshot managementSnapshot) error {
	capable := make(map[string]bool, len(snapshot.Inbounds))
	for _, inbound := range snapshot.Inbounds {
		capable[inbound.Name] = inbound.Enabled && protocolQuotaEnforcement(inbound.Protocol)
	}
	quota := make(map[string]bool, len(snapshot.Clients))
	for _, item := range snapshot.Clients {
		quota[item.ID] = item.QuotaBytes != nil
	}
	for _, binding := range snapshot.Bindings {
		if !binding.Enabled || !quota[binding.ClientID] {
			continue
		}
		if !capable[binding.InboundID] {
			return fmt.Errorf("client %q: quota enforcement is unsupported for inbound %s", binding.ClientID, binding.InboundID)
		}
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
		// Depletion is derived quota state: it is carried forward only while
		// the restored configuration still has a quota. Restoring "no quota"
		// is an explicit transition to unlimited, exactly like a live quota
		// clear (normalizeClearedQuota) — OR-ing the live flag onto it would
		// commit the depleted-without-quota state every writer refuses
		// (#1350).
		snapshotClient.Depleted = snapshotClient.QuotaBytes != nil && (snapshotClient.Depleted || live.Depleted)
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
			IPLimit: item.IPLimit, Notes: item.Notes, Depleted: item.Depleted, CreatedAt: item.CreatedAt,
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
	state.pruneHy2AuthSecretsLocked()
	// The inbound set and the committed client table were both replaced —
	// tracker entries for names/IDs that did not survive can never reconcile
	// (#1206).
	state.pruneHy2AdmissionInboundsLocked()
	state.pruneHy2AdmissionToLiveClients()
	state.rules = cloned.Rules
	state.routingPreset = cloned.RoutingPreset
	state.routingSource = cloned.RoutingSource
	state.warp = cloned.Warp
	// state.users deliberately keeps the live accounts: the committed
	// snapshot was already grafted with them, so assigning snapshot users
	// here would resurrect revoked panel credentials (#1094). The
	// provisioned latch re-arms from the surviving set — a rollback can
	// never un-provision the instance either (#1100).
	state.noteUsersProvisionedLocked()
}

func roleFromRequestContext(r *http.Request) string {
	role, _ := r.Context().Value(contextKeyRole).(string)
	return role
}
