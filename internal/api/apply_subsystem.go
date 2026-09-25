package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/protocols/hysteria2"
	"github.com/mikkelchokolate/Veil/internal/protocols/mieru"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

type clientBackgroundWorkers struct {
	collector  *client.Collector
	reconciler *client.Reconciler
	expiration *expirationReconciler
}

func detachClientBackgroundWorkers(s *managementState) clientBackgroundWorkers {
	workers := clientBackgroundWorkers{collector: s.trafficCollector, reconciler: s.trafficReconciler, expiration: s.expirationReconciler}
	s.trafficCollector = nil
	s.trafficReconciler = nil
	s.expirationReconciler = nil
	return workers
}

func stopClientBackgroundWorkers(workers clientBackgroundWorkers) {
	if workers.collector != nil {
		workers.collector.Stop()
	}
	if workers.reconciler != nil {
		workers.reconciler.Stop()
	}
	if workers.expiration != nil {
		workers.expiration.Stop()
	}
}

func closeClientDatabase(s *managementState) error {
	s.clientLifecycleMu.Lock()
	defer s.clientLifecycleMu.Unlock()
	if s.db == nil {
		return nil
	}
	db := s.db
	runner := s.applyRunner
	tokenStore := s.tokenStore
	s.db = nil
	s.applyRevisions = nil
	s.applyJobs = nil
	s.applySnapshots = nil
	s.applyRunner = nil
	s.clientService = nil
	s.clientRepo = nil
	s.clientCreds = nil
	s.clientMigrator = nil
	s.tokenStore = nil
	s.subRenderer = nil
	s.trafficStore = nil
	if runner != nil {
		runner.Close()
	}
	if tokenStore != nil {
		tokenStore.Close()
	}
	return db.Close()
}

// closeClientSubsystem is retained for package tests and routes every teardown
// through the same synchronized lifecycle used by production shutdown.
func closeClientSubsystem(s *managementState) error {
	return s.Close()
}

// initApplySubsystem opens the normalized SQLite store next to the state file
// and wires durable revisions and apply jobs. It is a no-op when no StatePath
// is configured (in-memory/test servers) — revision/apply tracking then
// degrades gracefully and apply falls back to the legacy synchronous path.
func initApplySubsystem(s *managementState) {
	initApplySubsystemMode(s, false)
}

// initApplySubsystemDeferredRecovery is the under-s.mu variant used by
// ReloadLocked callers: runner construction must not synchronously resume
// recovery-pending jobs, because that resume invokes the apply executor —
// which locks s.mu to load the pinned revision snapshot — and re-entering a
// held mutex on the same goroutine deadlocked the whole panel (#1067). The
// runner's monitor goroutine performs the same resume asynchronously.
func initApplySubsystemDeferredRecovery(s *managementState) {
	initApplySubsystemMode(s, true)
}

func initApplySubsystemMode(s *managementState, deferStartupRecovery bool) {
	s.clientLifecycleMu.Lock()
	defer s.clientLifecycleMu.Unlock()
	if s.statePath == "" {
		return
	}
	dbPath := filepath.Join(filepath.Dir(s.statePath), "veil.db")
	opener := storage.Open
	if s.databaseOpener != nil {
		opener = s.databaseOpener
	}
	db, err := opener(dbPath)
	if err != nil {
		s.storageDegradedErr = err
		log.Printf("apply subsystem: persistent SQLite unavailable: %v", err)
		return
	}
	s.storageDegradedErr = nil
	s.db = db
	if s.idempotency != nil {
		s.idempotency.mu.Lock()
		s.idempotency.db = db
		s.idempotency.mu.Unlock()
	}
	s.applyRevisions = apply.NewRevisionStore(s.db)
	s.applyJobs = apply.NewJobStore(s.db)
	s.applySnapshots = apply.NewSnapshotStore(s.db)
	if deferStartupRecovery {
		s.applyRunner = apply.NewRunnerDeferredStartupRecovery(s.applyRevisions, s.applyJobs, apply.ContextExecutorFunc(s.executeApplyRevisionContext))
	} else {
		s.applyRunner = apply.NewRunner(s.applyRevisions, s.applyJobs, apply.ContextExecutorFunc(s.executeApplyRevisionContext))
	}
	var runtimeStatus string
	if err := s.db.QueryRow(`SELECT status FROM runtime_verification WHERE id=1`).Scan(&runtimeStatus); err != nil {
		s.storageDegradedErr = fmt.Errorf("read runtime verification state: %w", err)
		s.runtimeVerificationUnknown = true
	} else {
		s.runtimeVerificationUnknown = runtimeStatus != "verified"
	}
}

// bindingCapabilityForInbound resolves the protocol capabilities of the named
// inbound for the enriched client binding read model. Returns nil when the
// inbound or its protocol is unknown. Per-client credential and expiry
// enforcement are advertised only when the protocol's runtime authenticates
// clients with distinct credentials (PerClientCredentialEnforcer).
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
	// Link rendering (ClientAccessProvider) does not imply runtime
	// enforcement: olcRTC emits a link per client but every client shares the
	// inbound-wide key, so per-client rotation/expiry must not be advertised
	// (audit #309).
	perClient := protocols.EnforcesPerClientCredentials(p)
	return &client.BindingCapability{
		Protocol:             meta.Protocol,
		Transports:           meta.Transports,
		PerClientCredentials: perClient,
		RequiresCaddy:        meta.RequiresCaddy,
		// Hysteria2 and mieru expose per-user counters the collector can read;
		// quota enforcement stays hysteria2-only until mieru limits are wired
		// into its rendered user table.
		TrafficAccounting:     meta.Protocol == "hysteria2" || meta.Protocol == "mieru",
		QuotaEnforcement:      meta.Protocol == "hysteria2",
		CredentialKinds:       []string{"password"},
		ExpirationEnforcement: perClient,
	}
}

// same SQLite store. It must run AFTER the secrets cipher is loaded because
// the credential store encrypts at rest with it. No-op when no store/cipher.
func initClientSubsystem(s *managementState) {
	s.clientLifecycleMu.Lock()
	defer s.clientLifecycleMu.Unlock()
	if s.db == nil || s.cipher == nil || s.clientSubsystemStopping {
		return
	}
	clientRepo := client.NewRepository(s.db)
	s.clientRepo = clientRepo
	clientCreds := client.NewCredentialStore(s.db, s.cipher)
	s.clientCreds = clientCreds
	// No ApplyNotifier: mutations never fire-and-forget applies from inside
	// the service. The HTTP handler runs the unified orchestration exactly
	// once per committed mutation (revision bump + snapshot + one job) and
	// returns that exact revision/job in the response.
	s.clientService = client.NewService(clientRepo, clientCreds).
		WithInboundLookup(s.bindingCapabilityForInbound).
		WithApplyReadiness(s.clientApplyReadiness)
	s.clientMigrator = client.NewMigrator(clientRepo, clientCreds, client.WithIncludeDisabled())
	s.tokenStore = client.NewTokenStore(s.db).WithCipher(s.cipher)
	s.subRenderer = client.NewSubscriptionRenderer(clientRepo, clientCreds)
	if s.trafficStore == nil {
		s.trafficStore = client.NewTrafficStore(s.db)
	}
	startCollector := false
	if s.trafficCollector == nil {
		s.trafficCollector = client.NewCollector(s.trafficStore, 0, nil)
		startCollector = true
	}
	// Blocker A2: quota reconciliation is a REAL configuration mutation — the
	// depleted flag changes the rendered runtime config. It therefore routes
	// through the same orchestration as every other client mutation: the flag
	// flip, the desired-revision bump, and the immutable snapshot commit in
	// ONE SQLite transaction, and exactly one apply job runs for the new
	// revision. Before this the reconciler flipped the flag and ran a bare
	// apply with no revision/snapshot, so the pinned snapshot never contained
	// the depleted state.
	startReconciler := false
	if s.trafficReconciler == nil {
		s.trafficReconciler = client.NewTransactionalReconciler(clientRepo, s.trafficStore, 0, func(mutation client.QuotaMutation) error {
			if err := s.enforceQuotaMutation(mutation); err != nil {
				log.Printf("traffic: reconcile client %s depleted=%v reset=%v: %v", mutation.ClientID, mutation.Depleted, mutation.ResetPeriod, err)
				return err
			}
			return nil
		})
		startReconciler = true
	}
	// A9: register real TrafficProviders for supported protocols. Hysteria2
	// reads per-user stats from the runtime's stats file. Other protocols can
	// register their own providers here.
	s.registerTrafficProvidersLocked()
	// Start the collector and reconciler so quota depletion is reconciled and
	// counters are collected. With zero providers CollectOnce is a no-op and
	// the summary endpoint honestly reports state="unsupported" (no fake zeros).
	if startCollector {
		s.trafficCollector.Start()
	}
	if startReconciler {
		s.trafficReconciler.Start()
	}
	if s.expirationReconciler == nil {
		s.expirationReconciler = newExpirationReconciler(s)
		s.expirationReconciler.Start()
	}
}

// registerTrafficProvidersLocked creates and registers TrafficProviders for
// all supported protocols with live runtime roots. Caller must hold s.mu.
func (s *managementState) registerTrafficProvidersLocked() {
	if s.trafficCollector == nil || s.clientRepo == nil {
		return
	}
	providers, err := s.buildTrafficProvidersLocked()
	if err != nil {
		log.Printf("traffic: preserve existing providers after refresh failure: %v", err)
		s.trafficCollector.MarkDegraded("collector", err)
		return
	}
	if err := s.trafficCollector.ResetProductionProviders(providers); err != nil {
		s.trafficCollector.MarkDegraded("collector", err)
	}
}

// buildTrafficProvidersLocked constructs TrafficProviders for the live applied
// configuration. A database read failure returns an error so the caller can
// keep the last working provider set. Caller must hold s.mu.
func (s *managementState) buildTrafficProvidersLocked() ([]client.TrafficProvider, error) {
	if s.trafficCollector == nil || s.clientRepo == nil {
		return nil, nil
	}
	allBindings, err := s.clientRepo.AllBindings()
	if err != nil {
		return nil, fmt.Errorf("list bindings for provider: %w", err)
	}
	allClients, err := s.clientRepo.AllClients()
	if err != nil {
		return nil, fmt.Errorf("list clients for provider: %w", err)
	}
	inbounds, settings := s.liveTrafficObservationConfigLocked()
	providers := []client.TrafficProvider{}
	for _, inbound := range inbounds {
		if inbound.Protocol != "hysteria2" || !inbound.Enabled {
			continue
		}
		bindings := trafficIdentityMap(inbound.Name, inbound.Profiles, allBindings, allClients)
		endpoint := fmt.Sprintf("http://127.0.0.1:%d/traffic", inbound.Port)
		secret := hysteria2.TrafficStatsSecret(settings, inbound)
		// The unit authenticates with the trafficStats block of its published
		// rendered config, which can belong to a newer revision than the last
		// applied snapshot (a partially failed apply still restarts units with
		// what it already rendered). Read the live artifact so the provider
		// sends the credential the runtime actually serves; keep the
		// snapshot-derived values when nothing is published yet.
		liveConfig := filepath.Join(s.liveRoot, "hysteria2", inbound.Name+".yaml")
		if listen, renderedSecret, ok, err := hysteria2.RenderedTrafficStats(liveConfig); err != nil {
			log.Printf("traffic: cannot read rendered hysteria2 config for inbound %s, using snapshot credential: %v", inbound.Name, err)
		} else if ok {
			endpoint = "http://" + listen + "/traffic"
			secret = renderedSecret
		}
		provider := hysteria2.NewAuthenticatedStatsProvider("hysteria2:"+inbound.Name, endpoint, secret, bindings)
		providers = append(providers, provider)
		log.Printf("traffic: registered authenticated hysteria2 provider for inbound %s", inbound.Name)
	}
	// One mieru daemon serves every mieru inbound, so a single provider reads
	// `mita get metrics` and maps the whole users table. Rendered usernames
	// are unique across mieru inbounds (the config builder rejects
	// duplicates), so the merged identity map cannot collide.
	mieruIdentities := make(map[string]string)
	mieruEnabled := false
	for _, inbound := range inbounds {
		if inbound.Protocol != "mieru" || !inbound.Enabled {
			continue
		}
		mieruEnabled = true
		for identity, bindingID := range trafficIdentityMap(inbound.Name, inbound.Profiles, allBindings, allClients) {
			if prev, exists := mieruIdentities[identity]; exists && prev != bindingID {
				log.Printf("traffic: mieru runtime identity %q claimed by bindings %s and %s — keeping first", identity, prev, bindingID)
				continue
			}
			mieruIdentities[identity] = bindingID
		}
	}
	if mieruEnabled {
		providers = append(providers, mieru.NewStatsProvider("mieru:server", mieruIdentities))
		log.Printf("traffic: registered mieru metrics provider for shared daemon")
	}
	return providers, nil
}

func (s *managementState) liveTrafficObservationConfigLocked() ([]Inbound, Settings) {
	if s.applyTrackingEnabled() && s.applyRevisions != nil && s.applySnapshots != nil {
		revisions, err := s.applyRevisions.Get()
		if err == nil && revisions.Applied > 0 {
			payload, err := s.applySnapshots.Load(revisions.Applied)
			if err == nil {
				var snapshot managementSnapshot
				if err := json.Unmarshal(payload, &snapshot); err == nil {
					return snapshot.Inbounds, snapshot.Settings
				}
			}
		}
	}
	return s.inbounds, s.settings
}

// RefreshTrafficProviders re-registers traffic providers so attribution tracks
// client/binding/credential/inbound changes without a restart. Safe to call
// after any client mutation or apply.
func (s *managementState) RefreshTrafficProviders() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerTrafficProvidersLocked()
}

// applyTrackingEnabled reports whether durable revisions/jobs are available.
func (s *managementState) applyTrackingEnabled() bool {
	return s.applyRunner != nil && s.applyRevisions != nil && s.applyJobs != nil
}

func (s *managementState) ensureRunnableRevision() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	revisions, err := s.applyRevisions.Get()
	if err != nil {
		return 0, err
	}
	if revisions.Desired == 0 && !s.applySnapshots.Has(0) {
		return s.bumpDesiredRevisionLocked()
	}
	return revisions.Desired, nil
}

func (s *managementState) convergeRevisionForSideEffect(ctx context.Context, revision uint64) (apply.Result, error) {
	revisions, err := s.applyRevisions.Get()
	if err != nil {
		return apply.Result{}, err
	}
	if revisions.Applied != revision {
		return s.executeApplyRevisionContext(ctx, revision)
	}
	// Revision equality is not runtime evidence (#545): a unit may have died
	// or drifted after the revision was marked applied. Before reporting the
	// revision live, probe the units its snapshot expects to be active. When
	// the probe cannot prove convergence — no evidence path or an unhealthy
	// unit — fall through to a real apply instead of a false Success.
	healthy, err := s.revisionRuntimeHealthy(ctx, revision)
	if err != nil {
		return apply.Result{}, err
	}
	if !healthy {
		return s.executeApplyRevisionContext(ctx, revision)
	}
	return apply.Result{
		Success: true, Disposition: apply.ApplyDispositionRuntimeConverged, MarkRevisionLive: true,
	}, nil
}

// revisionRuntimeHealthy probes the health-checked runtime units of the
// revision's pinned snapshot against the live service manager. It returns
// true only when every expected unit reports active — real evidence that the
// applied revision is still converged. A missing snapshot, missing privileged
// helper, or any non-active unit reports false so the caller runs a real
// apply rather than trusting revision equality (#545).
func (s *managementState) revisionRuntimeHealthy(ctx context.Context, revision uint64) (bool, error) {
	s.mu.Lock()
	snapshot, err := s.loadRevisionSnapshotLocked(revision)
	s.mu.Unlock()
	if err != nil {
		return false, nil
	}
	var units []string
	for _, runtime := range NewManagedRuntimeCatalogForSnapshot(
		snapshot.Settings, snapshot.Inbounds, snapshot.Warp).Runtimes() {
		if runtime.HealthCheckAfter && runtime.Unit != "" {
			units = append(units, runtime.Unit)
		}
	}
	if len(units) == 0 {
		// The revision declares no health-checked runtime units — nothing to
		// converge, so the applied marker is the only state to maintain.
		return true, nil
	}
	if s.privileged == nil && !s.privilegedLocal {
		return false, nil
	}
	actions := make([]ServiceActionResult, 0, len(units))
	for _, unit := range units {
		actions = append(actions, ServiceActionResult{
			Name: unit, Command: []string{"systemctl", "restart", unit}, Success: true,
		})
	}
	checks := NewManagementApplyContextWithContext(s, ctx).checkServiceHealth(actions)
	if len(checks) == 0 {
		return false, nil
	}
	for _, check := range checks {
		if !check.Healthy {
			return false, nil
		}
	}
	return true, nil
}

func completeSideEffectPublication(ctx context.Context, details apply.PublicationDetails) error {
	for _, phase := range []string{
		apply.PublicationPhaseSideEffectCommitted,
		apply.PublicationPhaseSideEffectVerified,
	} {
		if err := apply.AdvanceRuntimePublication(ctx, phase, details); err != nil {
			return err
		}
	}
	return nil
}

// bumpDesiredRevisionLocked records a committed configuration mutation for the
// STATE-FILE path (settings/inbounds/routing/warp/users): the desired revision
// bump and the immutable snapshot commit in ONE SQLite transaction, and any
// error is returned so the mutation fails honestly instead of leaving a
// committed state file with no pinned revision. (The normalized client path
// instead folds client rows + revision + snapshot into a single transaction
// via commitClientMutationLocked.) Caller must hold s.mu. Returns the new
// desired revision, or 0 when tracking is disabled.
func (s *managementState) bumpDesiredRevisionLocked(stateDigests ...string) (uint64, error) {
	if !s.applyTrackingEnabled() {
		return 0, nil
	}
	if len(stateDigests) > 1 {
		return 0, fmt.Errorf("apply subsystem: multiple state digests supplied")
	}
	stateDigest := ""
	if len(stateDigests) == 1 {
		stateDigest = stateDigests[0]
	} else if s.statePath != "" {
		var err error
		stateDigest, err = stateFileDigest(s.statePath)
		if err != nil {
			return 0, fmt.Errorf("apply subsystem: digest state file: %w", err)
		}
	}
	// Build + encrypt + marshal the snapshot BEFORE opening the transaction.
	// The snapshot reads the client tables through the connection pool in
	// autocommit; doing that INSIDE the tx would deadlock the single-
	// connection SQLite pool (the tx holds the only connection). s.mu
	// serializes all state mutations, so the rows read here are exactly the
	// committed state this revision records.
	var payload []byte
	if s.applySnapshots != nil {
		snap, snapshotBuildErr := s.snapshotLocked()
		if snapshotBuildErr != nil {
			return 0, fmt.Errorf("apply subsystem: build revision snapshot: %w", snapshotBuildErr)
		}
		if err := s.encryptSnapshot(&snap); err != nil {
			return 0, fmt.Errorf("apply subsystem: encrypt revision snapshot: %w", err)
		}
		var err error
		payload, err = json.Marshal(snap)
		if err != nil {
			return 0, fmt.Errorf("apply subsystem: marshal revision snapshot: %w", err)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("apply subsystem: begin revision tx: %w", err)
	}
	rev, err := apply.BumpDesiredTx(tx)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("apply subsystem: bump desired revision: %w", err)
	}
	// Record an immutable snapshot of the configuration committed as this
	// revision. Apply jobs for this revision render from the snapshot, never
	// from newer mutable state. Save is idempotent (first write wins). Secrets
	// were encrypted above so the snapshot never stores plaintext.
	if s.applySnapshots != nil {
		var snapshotErr error
		if s.statePath == "" {
			snapshotErr = apply.SaveSnapshotTx(tx, rev, payload)
		} else {
			snapshotErr = apply.SaveSnapshotTxBound(tx, rev, payload, stateDigest)
		}
		if snapshotErr != nil {
			_ = tx.Rollback()
			return 0, snapshotErr
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("apply subsystem: commit revision %d: %w", rev, err)
	}
	return rev, nil
}

func stateFileDigest(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return managementstate.EncodedStateSHA256(body), nil
}

// executeApplyRevision applies one immutable desired revision to the runtime.
// It loads a revision-pinned snapshot under the state mutex, then runs the
// filesystem/service workflow against a private execution state. New mutations
// may commit while this apply is in flight, but cannot change what it renders.
func (s *managementState) executeApplyRevision(revision uint64) (apply.Result, error) {
	return s.executeApplyRevisionContext(s.lifecycleContext(), revision)
}

func (s *managementState) executeApplyRevisionContext(operationContext context.Context, revision uint64) (apply.Result, error) {
	_, _, result, err := s.executeApplyRevisionRequestContext(operationContext, revision,
		ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
	if err == nil && !result.Success {
		message := result.ErrorMessage
		if message == "" {
			message = "apply workflow reported an unsuccessful result"
		}
		err = errors.New(message)
	}
	return result, err
}

func (s *managementState) executeApplyRevisionRequestContext(operationContext context.Context, revision uint64, request ApplyRequest) (ApplyResponse, int, apply.Result, error) {
	if operationContext == nil {
		operationContext = s.lifecycleContext()
	}
	s.mu.Lock()
	snapshot, err := s.loadRevisionSnapshotLocked(revision)
	if err != nil {
		s.mu.Unlock()
		result := apply.Result{Success: false, ErrorCode: "SNAPSHOT_UNAVAILABLE", ErrorMessage: err.Error()}
		return ApplyResponse{}, http.StatusInternalServerError, result, err
	}
	execution := s.newApplyExecutionStateLocked(snapshot)
	s.mu.Unlock()

	response, status, err := NewApplyWorkflow(NewManagementApplyContextWithContext(execution, operationContext)).RunLocked(request)

	s.mu.Lock()
	s.orphanedUnits = append([]string(nil), execution.orphanedUnits...)
	s.mu.Unlock()

	result := apply.Result{
		Success:    err == nil && status == http.StatusOK,
		RolledBack: response.RolledBack,
		RuntimeMutation: apply.RuntimeMutationOutcome{
			MutationStarted:        response.MutationStarted,
			ArtifactsChanged:       response.ArtifactsChanged,
			ServicesChanged:        response.ServicesChanged,
			FirewallChanged:        response.FirewallChanged,
			ArtifactsRestored:      response.ArtifactsRestored,
			ServicesRestored:       response.ServicesRestored,
			FirewallRestored:       response.FirewallRestored,
			PostRollbackHealthPass: response.PostRollbackHealthPass,
			RollbackComplete:       response.RollbackComplete,
			Ambiguous:              response.Ambiguous,
		},
	}
	if result.Success {
		switch {
		case response.Applied:
			result.Disposition = apply.ApplyDispositionRuntimeConverged
			result.MarkRevisionLive = true
		case response.LiveApplied:
			result.Disposition = apply.ApplyDispositionArtifactsCommitted
		default:
			result.Disposition = apply.ApplyDispositionStaged
		}
	}
	if err != nil {
		result.ErrorCode = "APPLY_ERROR"
		result.ErrorMessage = err.Error()
		return response, status, result, err
	}
	if status != http.StatusOK {
		result.ErrorCode = applyFailureCode(response)
		result.ErrorMessage = applyFailureMessage(response, status)
	}
	return response, status, result, nil
}

func (s *managementState) loadRevisionSnapshotLocked(revision uint64) (managementSnapshot, error) {
	if s.applySnapshots == nil {
		return managementSnapshot{}, fmt.Errorf("apply: snapshot store unavailable for revision %d", revision)
	}
	payload, err := s.applySnapshots.Load(revision)
	if err != nil {
		return managementSnapshot{}, fmt.Errorf("apply: no immutable snapshot for revision %d: %w", revision, err)
	}
	var snapshot managementSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return managementSnapshot{}, fmt.Errorf("apply: decode revision %d snapshot: %w", revision, err)
	}
	if snapshot.EffectiveAt == 0 {
		snapshot.EffectiveAt, err = s.applySnapshots.EffectiveAt(revision)
		if err != nil {
			return managementSnapshot{}, err
		}
	}
	if err := s.decryptSnapshot(&snapshot); err != nil {
		return managementSnapshot{}, fmt.Errorf("apply: decrypt revision %d snapshot: %w", revision, err)
	}
	return snapshot, nil
}

func (s *managementState) newApplyExecutionStateLocked(snapshot managementSnapshot) *managementState {
	return &managementState{
		lifecycleCtx:                   s.lifecycleCtx,
		statePath:                      s.statePath,
		applyRoot:                      s.applyRoot,
		liveRoot:                       s.liveRoot,
		systemdWantsDir:                s.systemdWantsDir,
		keyPath:                        s.keyPath,
		cipher:                         s.cipher,
		setup:                          snapshot.Setup,
		settings:                       snapshot.Settings,
		inbounds:                       append([]Inbound(nil), snapshot.Inbounds...),
		rules:                          append([]RoutingRule(nil), snapshot.Rules...),
		routingPreset:                  snapshot.RoutingPreset,
		routingSource:                  snapshot.RoutingSource,
		warp:                           snapshot.Warp,
		users:                          append([]User(nil), snapshot.Users...),
		orphanedUnits:                  append([]string(nil), s.orphanedUnits...),
		configurationValidator:         s.configurationValidator,
		enforceConfigurationValidation: s.enforceConfigurationValidation,
		privileged:                     s.privileged,
		privilegedLocal:                s.privilegedLocal,
		renderClients:                  append([]model.ClientSnapshot(nil), snapshot.Clients...),
		renderBindings:                 append([]model.BindingSnapshot(nil), snapshot.Bindings...),
		renderCredentials:              append([]model.CredentialSnapshot(nil), snapshot.Credentials...),
		renderEffectiveAt:              snapshot.EffectiveAt,
	}
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
	prev, err := s.snapshotLocked()
	if err != nil {
		return nil, fmt.Errorf("apply: capture mutable state before pinned render: %w", err)
	}
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
	s.renderEffectiveAt = snap.EffectiveAt
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
