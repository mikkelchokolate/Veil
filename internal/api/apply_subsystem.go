package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/protocols/hysteria2"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

// applyNotifierFunc adapts a function to client.ApplyNotifier.
type applyNotifierFunc func(kind, id string)

func (f applyNotifierFunc) NotifyMutation(kind, id string) { f(kind, id) }

// initApplySubsystem opens the normalized SQLite store next to the state file
// and wires durable revisions and apply jobs. It is a no-op when no StatePath
// is configured (in-memory/test servers) — revision/apply tracking then
// degrades gracefully and apply falls back to the legacy synchronous path.
func initApplySubsystem(s *managementState) {
	if s.statePath == "" {
		return
	}
	dbPath := filepath.Join(filepath.Dir(s.statePath), "veil.db")
	db, err := storage.Open(dbPath)
	if err != nil {
		// A broken store must not prevent the panel from serving; log and run
		// without revision tracking rather than refusing to start.
		log.Printf("apply subsystem: open %s: %v (revision tracking disabled)", dbPath, err)
		return
	}
	s.db = db
	s.applyRevisions = apply.NewRevisionStore(s.db)
	s.applyJobs = apply.NewJobStore(s.db)
	s.applySnapshots = apply.NewSnapshotStore(s.db)
	s.applyRunner = apply.NewRunner(s.applyRevisions, s.applyJobs, s.executeApplyRevision)
}

// bindingCapabilityForInbound resolves the protocol capabilities of the named
// inbound for the enriched client binding read model. Returns nil when the
// inbound or its protocol is unknown. Per-client credential support is taken
// from the protocol's ClientAccessProvider capability.
func (s *managementState) bindingCapabilityForInbound(inboundID string) *client.BindingCapability {
	s.mu.Lock()
	var proto string
	for _, in := range s.inbounds {
		if in.Name == inboundID {
			proto = in.Protocol
			break
		}
	}
	s.mu.Unlock()
	if proto == "" {
		return nil
	}
	reg := protocols.NewRegistry()
	p, ok := reg.Get(proto)
	if !ok {
		return nil
	}
	meta := protocols.MetadataOf(p)
	_, perClient := protocols.AsClientAccessProvider(p)
	return &client.BindingCapability{
		Protocol:             meta.Protocol,
		Transports:           meta.Transports,
		PerClientCredentials: perClient,
		RequiresCaddy:        meta.RequiresCaddy,
	}
}

// same SQLite store. It must run AFTER the secrets cipher is loaded because
// the credential store encrypts at rest with it. No-op when no store/cipher.
func initClientSubsystem(s *managementState) {
	if s.db == nil || s.cipher == nil {
		return
	}
	clientRepo := client.NewRepository(s.db)
	s.clientRepo = clientRepo
	clientCreds := client.NewCredentialStore(s.db, s.cipher)
	s.clientService = client.NewService(clientRepo, clientCreds, applyNotifierFunc(func(kind, id string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.autoApplyResultLocked(nil, "system")
	})).WithInboundLookup(s.bindingCapabilityForInbound)
	s.clientMigrator = client.NewMigrator(clientRepo, clientCreds)
	s.tokenStore = client.NewTokenStore(s.db)
	s.subRenderer = client.NewSubscriptionRenderer(clientRepo, clientCreds)
	s.trafficStore = client.NewTrafficStore(s.db)
	s.trafficCollector = client.NewCollector(s.trafficStore, 0, nil)
	s.trafficReconciler = client.NewReconciler(clientRepo, s.trafficStore, 0, func(clientID string, depleted bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.autoApplyResultLocked(nil, "system")
	})
	// A9: register real TrafficProviders for supported protocols. Hysteria2
	// reads per-user stats from the runtime's stats file. Other protocols can
	// register their own providers here.
	s.registerTrafficProvidersLocked()
	// Start the collector and reconciler so quota depletion is reconciled and
	// counters are collected. With zero providers CollectOnce is a no-op and
	// the summary endpoint honestly reports state="unsupported" (no fake zeros).
	s.trafficCollector.Start()
	s.trafficReconciler.Start()
}

// registerTrafficProvidersLocked creates and registers TrafficProviders for
// all supported protocols with live runtime roots. Caller must hold s.mu.
func (s *managementState) registerTrafficProvidersLocked() {
	if s.trafficCollector == nil || s.clientRepo == nil {
		return
	}
	// Build client username -> bindingID map for attribution.
	bindings := make(map[string]string)
	clients, err := s.clientRepo.AllClients()
	if err != nil {
		log.Printf("traffic: list clients for provider bindings: %v", err)
		return
	}
	allBindings, err := s.clientRepo.AllBindings()
	if err != nil {
		log.Printf("traffic: list bindings for provider: %v", err)
		return
	}
	clientNameByID := make(map[string]string, len(clients))
	for _, c := range clients {
		clientNameByID[c.ID] = c.Name
	}
	for _, b := range allBindings {
		if name, ok := clientNameByID[b.ClientID]; ok {
			bindings[name] = b.ID
		}
	}
	// Register hysteria2 provider if any hysteria2 inbound exists.
	for _, in := range s.inbounds {
		if in.Protocol != "hysteria2" || !in.Enabled {
			continue
		}
		statsPath := hysteria2.StatsFilePath(s.liveRoot, in.Name)
		provider := hysteria2.NewStatsProvider("hysteria2:"+in.Name, statsPath, bindings)
		s.trafficCollector.Register(provider)
		log.Printf("traffic: registered hysteria2 provider for inbound %s (stats: %s)", in.Name, statsPath)
	}
}

// stopTrafficSubsystem halts the periodic collector/reconciler. Safe to call
// multiple times.
func (s *managementState) stopTrafficSubsystem() {
	if s.trafficCollector != nil {
		s.trafficCollector.Stop()
	}
	if s.trafficReconciler != nil {
		s.trafficReconciler.Stop()
	}
}

// applyTrackingEnabled reports whether durable revisions/jobs are available.
func (s *managementState) applyTrackingEnabled() bool {
	return s.applyRunner != nil && s.applyRevisions != nil && s.applyJobs != nil
}

// bumpDesiredRevisionLocked records a committed configuration mutation. Caller
// must hold s.mu. Returns the new desired revision, or 0 when tracking is
// disabled. Errors are logged but never fail the mutation that triggered them.
func (s *managementState) bumpDesiredRevisionLocked() uint64 {
	if !s.applyTrackingEnabled() {
		return 0
	}
	rev, err := s.applyRevisions.BumpDesired()
	if err != nil {
		log.Printf("apply subsystem: bump desired revision: %v", err)
		return 0
	}
	// Record an immutable snapshot of the configuration committed as this
	// revision. Apply jobs for this revision render from the snapshot, never
	// from newer mutable state. Save is idempotent (first write wins). Secrets
	// are encrypted before persistence so the snapshot never stores plaintext.
	if s.applySnapshots != nil {
		snap := s.snapshotLocked()
		if err := s.encryptSnapshot(&snap); err != nil {
			log.Printf("apply subsystem: encrypt revision %d snapshot: %v", rev, err)
		} else if payload, serr := json.Marshal(snap); serr == nil {
			if werr := s.applySnapshots.Save(rev, payload); werr != nil {
				log.Printf("apply subsystem: save revision %d snapshot: %v", rev, werr)
			}
		}
	}
	return rev
}

// executeApplyRevision applies one immutable desired revision to the runtime.
// It is the apply.ExecuteFunc seam for the Runner. IMPORTANT: callers invoke
// the Runner while already holding s.mu (via autoApplyResultLocked), so this
// function must NOT acquire s.mu again (the mutation that produced the
// revision is already committed and the apply runner serializes execution).
func (s *managementState) executeApplyRevision(revision uint64) (apply.Result, error) {
	// Pin the job to the immutable snapshot recorded for this revision. While
	// s.mu is held by the caller no newer mutation can interleave, but a retry
	// or reconcile may run for an OLDER revision after newer ones committed —
	// in that case render from the pinned snapshot, not live state.
	restore, err := s.pinStateToRevisionLocked(revision)
	if err != nil {
		// A3: no immutable snapshot → apply must fail, not fall back to current
		// mutable state (which would violate revision immutability).
		return apply.Result{Success: false, ErrorCode: "SNAPSHOT_UNAVAILABLE", ErrorMessage: err.Error()}, err
	}
	if restore != nil {
		defer restore()
	}
	response, status, err := NewApplyWorkflow(NewManagementApplyContext(s)).
		RunLocked(ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	res := apply.Result{Success: err == nil && status == http.StatusOK, RolledBack: response.RolledBack}
	if err != nil {
		res.ErrorCode = "APPLY_ERROR"
		res.ErrorMessage = err.Error()
		return res, err
	}
	if status != http.StatusOK {
		res.ErrorCode = applyFailureCode(response)
		res.ErrorMessage = applyFailureMessage(response, status)
	}
	return res, nil
}

// pinStateToRevisionLocked swaps the live mutable configuration for the
// immutable snapshot recorded for the given revision and returns a restore
// function. If no snapshot exists (legacy revision recorded before snapshots,
// or the revision is the latest), it returns nil and the executor renders
// current state. Caller must hold s.mu; the returned closure must run before
// releasing it.
func (s *managementState) pinStateToRevisionLocked(revision uint64) (func(), error) {
	if s.applySnapshots == nil {
		return nil, fmt.Errorf("apply: snapshot store unavailable for revision %d", revision)
	}
	payload, err := s.applySnapshots.Load(revision)
	if err != nil {
		// A3: FORBIDDEN fallback removed. For tracked revisions we must render
		// from the immutable snapshot, never from current mutable state. If the
		// snapshot is missing or corrupt the apply must fail, not silently use
		// newer state that would violate immutability.
		return nil, fmt.Errorf("apply: no immutable snapshot for revision %d: %w", revision, err)
	}
	var snap managementSnapshot
	if err := json.Unmarshal(payload, &snap); err != nil {
		return nil, fmt.Errorf("apply: decode revision %d snapshot: %w", revision, err)
	}
	// Snapshots are stored encrypted; decrypt before rendering.
	if err := s.decryptSnapshot(&snap); err != nil {
		return nil, fmt.Errorf("apply: decrypt revision %d snapshot: %w", revision, err)
	}
	// Capture live state so we can restore it after the pinned render.
	prev := s.snapshotLocked()
	applyRenderSnapshot(s, snap)
	return func() { applyRenderSnapshot(s, prev) }, nil
}

// applyRenderSnapshot overwrites the live mutable configuration with a
// snapshot for rendering. Unlike managementstate.ApplySnapshot (which merges
// defaults and skips empty fields for state-file load), this performs a full
// field replacement: an apply job for revision N must render EXACTLY the
// configuration committed as revision N, not a merge with newer state.
func applyRenderSnapshot(s *managementState, snap managementSnapshot) {
	s.setup = snap.Setup
	s.settings = snap.Settings
	s.inbounds = snap.Inbounds
	s.rules = snap.Rules
	s.routingPreset = snap.RoutingPreset
	s.routingSource = snap.RoutingSource
	s.warp = snap.Warp
	s.users = snap.Users
	// A3: normalized client state is part of the immutable snapshot. The
	// renderer must see exactly the clients/bindings/credentials committed as
	// this revision, not current mutable SQLite state.
	s.renderClients = snap.Clients
	s.renderBindings = snap.Bindings
	s.renderCredentials = snap.Credentials
}

// applyFailureCode maps an unsuccessful apply response to a stable error code.
func applyFailureCode(r ApplyResponse) string {
	if r.RolledBack {
		return "ROLLED_BACK"
	}
	for _, h := range r.HealthChecks {
		if !h.Healthy {
			return "HEALTH_CHECK_FAILED"
		}
	}
	for _, a := range r.ServiceActions {
		if !a.Success {
			return "SERVICE_ACTION_FAILED"
		}
	}
	for _, v := range r.Validations {
		if !v.Valid {
			return "VALIDATION_FAILED"
		}
	}
	return "APPLY_FAILED"
}

func applyFailureMessage(r ApplyResponse, status int) string {
	for _, h := range r.HealthChecks {
		if !h.Healthy {
			return "health check failed for " + h.Name + ": " + firstNonEmpty(h.Error, h.Output)
		}
	}
	for _, a := range r.ServiceActions {
		if !a.Success {
			return "service action failed for " + a.Name + ": " + firstNonEmpty(a.Error, a.Output)
		}
	}
	for _, v := range r.Validations {
		if !v.Valid {
			return "config validation failed for " + v.Name + ": " + firstNonEmpty(v.Error, v.Output)
		}
	}
	return "apply did not succeed (status " + http.StatusText(status) + ")"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
