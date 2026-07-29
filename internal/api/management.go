package api

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/applyhistory"
	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	"github.com/mikkelchokolate/Veil/internal/service"
)

var stagedConfigValidator = func(paths []string) []ConfigValidationResult {
	return newPluginStagedConfigValidator(generatedconfig.RunFixedConfigValidation).Validate(paths)
}
var serviceActionRunner = func(command []string) ServiceActionResult {
	return service.RunFixedServiceAction(command, service.NewCommandPolicy(NewManagedRuntimeCatalog()), nil)
}
var serviceHealthChecker = func(serviceName string) ServiceHealthResult {
	return service.RunFixedServiceHealthCheck(serviceName, service.NewCommandPolicy(NewManagedRuntimeCatalog()), nil)
}

// autoApplyAfterMutation controls whether management state mutations
// (inbounds, settings, routing rules/presets, WARP) automatically trigger a
// full live + services apply. Tests can disable this to avoid side effects
// when exercising CRUD in isolation.
var autoApplyAfterMutation = true

// Reloader is an optional interface for runtime state reload.
type Reloader interface {
	Reload() error
}

func (s *managementState) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/protocols", s.handleProtocols)
	mux.HandleFunc("/api/inbounds", s.handleInbounds)
	mux.HandleFunc("/api/inbounds/", s.handleInboundByName)
	mux.HandleFunc("/api/routing/rules", s.handleRoutingRules)
	mux.HandleFunc("/api/routing/rules/", s.handleRoutingRuleByName)
	mux.HandleFunc("/api/routing/presets", s.handleRoutingPresets)
	mux.HandleFunc("/api/routing/presets/", s.handleRoutingPresetByName)
	mux.HandleFunc("/api/warp", s.handleWarp)
	s.registerProtocolRoomRoutes(mux)
	mux.HandleFunc("/api/client-links/qr", s.handleClientLinkQR)
	mux.HandleFunc("/api/client-links/subscription", s.handleClientLinksSubscription)
	mux.HandleFunc("/api/client-links", s.handleClientLinks)
	mux.HandleFunc("/api/firewall", s.handleFirewall)
	mux.HandleFunc("/api/validation", s.handleValidation)
	mux.HandleFunc("/api/apply/plan", s.handleApplyPlan)
	mux.HandleFunc("/api/apply/history", s.handleApplyHistory)
	mux.HandleFunc("/api/apply", s.handleApply)
	s.registerApplyRoutes(mux)
	s.registerClientV1Routes(mux)
	s.registerSubscriptionRoutes(mux)
	s.registerTrafficRoutes(mux)
	s.registerEventsRoutes(mux)
	mux.HandleFunc("/api/auth/login", s.handleLoginWithRevalidation)
	mux.HandleFunc("/api/auth/logout", s.handleLogoutWithSettingsSnapshot)
	mux.HandleFunc("/api/auth/status", s.handleEffectiveAuthStatus)
	mux.HandleFunc("/api/auth/locale", s.handleAuthLocale)
	mux.HandleFunc("/api/auth/sessions", s.handlePersistentAuthSessions)
	mux.HandleFunc("/api/admin/rotate-key", s.handleRotateKey)
	mux.HandleFunc("/api/audit", s.handleAudit)
	mux.HandleFunc("/api/backups", s.handleBackups)
	mux.HandleFunc("/api/backups/prune", s.handleBackupPrune)
	mux.HandleFunc("/api/backup-restore-jobs/", s.handleBackupRestoreJob)
	mux.HandleFunc("/api/backups/", s.handleBackupByName)
	mux.HandleFunc("/api/setup/status", s.handleSetupStatus)
	mux.HandleFunc("/api/setup/complete", s.handleSetupComplete)
	mux.HandleFunc("/api/users", s.handleUsersRouteWithAdminInvariant)
	mux.HandleFunc("/api/users/", s.handleReliableUserItemRoute)
}

// registerProtocolRoomRoutes registers per-protocol room generation routes for
// any plugin that implements protocols.RoomGenerator. This keeps the router
// agnostic of the concrete protocol set.
func (s *managementState) registerProtocolRoomRoutes(mux *http.ServeMux) {
	for _, p := range protocols.NewRegistry().All() {
		if _, ok := protocols.AsRoomGenerator(p); !ok {
			continue
		}
		protocol := p.Protocol()
		mux.HandleFunc("/api/"+protocol+"/room", s.handleProtocolRoom(protocol))
	}
}

func (s *managementState) withMutation(fn func(managementstate.Mutation) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(s.mutationLocked())
}

func (s *managementState) mutationLocked() managementstate.Mutation {
	return managementstate.NewMutation(managementstate.MutationTarget{
		Settings: &s.settings,
		Inbounds: &s.inbounds,
		Rules:    &s.rules,
		Warp:     &s.warp,
		Users:    &s.users,
	}, s.saveLocked)
}

func (s *managementState) applyHistoryPathLocked() string {
	return filepath.Join(s.applyRoot, "generated", "veil", "apply-history.json")
}

func (s *managementState) applyHistoryLocked() applyhistory.ApplyHistory {
	return applyhistory.NewApplyHistory(s.applyHistoryPathLocked(), applyhistory.MaxEntries)
}

func (s *managementState) livePathForStagedConfig(stagedPath string) (string, bool) {
	context := NewManagementApplyContext(s)
	return NewLiveConfigPromotion(s.applyRoot, context.reloadPromotedServices).LivePathForStagedConfig(stagedPath)
}

func (s *managementState) renderManagementConfigsLocked() (map[string]string, error) {
	inbounds, err := s.inboundsWithRuntimeCredentialsLocked()
	if err != nil {
		return nil, err
	}
	return NewManagementConfigRenderer(ManagementConfigInput{
		ApplyRoot: s.applyRoot, LiveRoot: s.liveRoot, Settings: s.settings,
		Inbounds: inbounds, Rules: s.rules, Warp: s.warp,
	}).Render()
}

// inboundsWithRuntimeCredentialsLocked returns the configured inbounds with
// per-client credentials resolved from the normalized Client+Binding+Credential
// store attached as runtime-only data, so the rendered live config includes
// normalized clients (not just legacy inbound-embedded profiles). Failures to
// resolve credentials are non-fatal: the inbound renders without them.
func (s *managementState) inboundsWithRuntimeCredentialsLocked() ([]Inbound, error) {
	if s.renderClients != nil || s.renderBindings != nil || s.renderCredentials != nil {
		return s.inboundsWithPinnedCredentialsLocked()
	}
	if s.clientService == nil {
		return s.inbounds, nil
	}
	out := make([]Inbound, len(s.inbounds))
	copy(out, s.inbounds)
	for i := range out {
		creds, err := s.clientService.CredentialsForInbound(out[i].Name)
		if err != nil {
			return nil, fmt.Errorf("resolve runtime credentials for inbound %s: %w", out[i].Name, err)
		}
		if len(creds) == 0 {
			continue
		}
		rc := make([]RuntimeCredential, 0, len(creds))
		for _, current := range creds {
			rc = append(rc, RuntimeCredential{Name: current.Name, Username: current.Username, Password: current.Password})
		}
		out[i].RuntimeCredentials = rc
	}
	return out, nil
}

// inboundsWithPinnedCredentialsLocked resolves runtime credentials from the
// pinned immutable snapshot (renderClients/renderBindings/renderCredentials)
// instead of live SQLite state. Used only during a pinned apply render.
func (s *managementState) inboundsWithPinnedCredentialsLocked() ([]Inbound, error) {
	out := make([]Inbound, len(s.inbounds))
	copy(out, s.inbounds)
	// Build lookup: bindingID -> client, bindingID -> credential.
	clientByID := make(map[string]model.ClientSnapshot, len(s.renderClients))
	for _, c := range s.renderClients {
		clientByID[c.ID] = c
	}
	credByBinding := make(map[string]model.CredentialSnapshot, len(s.renderCredentials))
	for _, cr := range s.renderCredentials {
		credByBinding[cr.BindingID] = cr
	}
	// Group enabled bindings by inbound.
	bindingsByInbound := make(map[string][]model.BindingSnapshot)
	for _, b := range s.renderBindings {
		if !b.Enabled {
			continue
		}
		bindingsByInbound[b.InboundID] = append(bindingsByInbound[b.InboundID], b)
	}
	for i := range out {
		bindings := bindingsByInbound[out[i].Name]
		if len(bindings) == 0 {
			continue
		}
		rc := make([]RuntimeCredential, 0, len(bindings))
		for _, b := range bindings {
			c, ok := clientByID[b.ClientID]
			if !ok || !c.Enabled || c.Depleted {
				continue
			}
			cred, ok := credByBinding[b.ID]
			if !ok {
				return nil, fmt.Errorf("enabled binding %s has no active credential", b.ID)
			}
			// Decrypt the pinned credential for rendering.
			ciphertext := string(cred.EncryptedValue)
			if !strings.HasPrefix(ciphertext, "ve1:") {
				return nil, fmt.Errorf("pinned credential %s ciphertext is invalid", cred.ID)
			}
			plaintext, err := s.cipher.Decrypt(ciphertext)
			if err != nil {
				return nil, fmt.Errorf("decrypt pinned credential %s: %w", cred.ID, err)
			}
			runtimeIdentity := b.RuntimeIdentity
			if runtimeIdentity == "" {
				runtimeIdentity = client.GenerateRuntimeIdentity(b.ID)
			}
			rc = append(rc, RuntimeCredential{Name: c.Name, Username: runtimeIdentity, Password: plaintext})
		}
		if len(rc) > 0 {
			out[i].RuntimeCredentials = rc
		}
	}
	return out, nil
}

func (s *managementState) snapshotLocked() managementSnapshot {
	return NewManagementStateLifecycle(s).SnapshotLocked()
}

func (s *managementState) encryptSnapshot(snapshot *managementSnapshot) error {
	return managementstate.EncryptSnapshot(snapshot, s.cipher)
}

func (s *managementState) decryptSnapshot(snapshot *managementSnapshot) error {
	return managementstate.DecryptSnapshot(snapshot, s.cipher)
}

func (s *managementState) load() error {
	return NewManagementStateLifecycle(s).Load()
}

// Reload re-reads the management state and encryption key from disk.
// It locks the state mutex during the reload. Returns an error if the
// state file or key file cannot be read, but leaves the previous state
// intact on failure.
func (s *managementState) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := NewManagementStateLifecycle(s).ReloadLocked()
	if err != nil {
		s.startupStateLoadFailed = true
		s.startupStateLoadErr = err
		s.allowDevAnonymous = false
		return err
	}
	s.startupStateLoadFailed = false
	s.startupStateLoadErr = nil
	return nil
}

// Close stops and joins every normalized-domain background worker before
// closing the SQLite store. RunLifecycle calls it after HTTP draining, while
// backup restore uses the same detach/stop/close primitives around its DB swap.
func (s *managementState) Close() error {
	if s.lifecycleCancel != nil {
		s.lifecycleCancel()
	}
	s.clientRequestMu.Lock()
	defer s.clientRequestMu.Unlock()
	s.mu.Lock()
	s.clientSubsystemStopping = true
	workers := detachClientBackgroundWorkers(s)
	hub := s.sse
	s.sse = nil
	limiter := s.httpRateLimiter
	s.httpRateLimiter = nil
	idempotency := s.idempotency
	s.idempotency = nil
	s.mu.Unlock()

	if hub != nil {
		hub.Close()
	}
	if limiter != nil {
		_ = limiter.Close()
	}
	if idempotency != nil {
		_ = idempotency.Close()
	}
	stopClientBackgroundWorkers(workers)

	s.mu.Lock()
	defer s.mu.Unlock()
	return closeClientDatabase(s)
}

func (s *managementState) lifecycleContext() context.Context {
	if s.lifecycleCtx != nil {
		return s.lifecycleCtx
	}
	return context.TODO()
}

func (s *managementState) saveLocked() error {
	return NewManagementStateLifecycle(s).SaveLocked()
}

func (s *managementState) logUserAction(r *http.Request, action string, target string, success bool, details string) {
	eventDetails := map[string]any(nil)
	if details != "" {
		eventDetails = map[string]any{"message": details}
	}
	s.recordRequestAudit(r, audit.Record{
		Action:  action,
		Target:  target,
		Success: success,
		Details: eventDetails,
	})
}
