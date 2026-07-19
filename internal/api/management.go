package api

import (
	"net/http"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/applyhistory"
	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
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
	return NewLiveConfigPromotion(s.applyRoot, context.reloadPromotedServicesLocked).LivePathForStagedConfig(stagedPath)
}

func (s *managementState) renderManagementConfigsLocked() (map[string]string, error) {
	return s.managementConfigRendererLocked().Render()
}

func (s *managementState) managementConfigRendererLocked() ManagementConfigRenderer {
	return NewManagementConfigRenderer(ManagementConfigInput{
		ApplyRoot: s.applyRoot,
		LiveRoot:  s.liveRoot,
		Settings:  s.settings,
		Inbounds:  s.inboundsWithRuntimeCredentialsLocked(),
		Rules:     s.rules,
		Warp:      s.warp,
	})
}

// inboundsWithRuntimeCredentialsLocked returns the configured inbounds with
// per-client credentials resolved from the normalized Client+Binding+Credential
// store attached as runtime-only data, so the rendered live config includes
// normalized clients (not just legacy inbound-embedded profiles). Failures to
// resolve credentials are non-fatal: the inbound renders without them.
func (s *managementState) inboundsWithRuntimeCredentialsLocked() []Inbound {
	if s.clientService == nil {
		return s.inbounds
	}
	out := make([]Inbound, len(s.inbounds))
	copy(out, s.inbounds)
	for i := range out {
		creds, err := s.clientService.CredentialsForInbound(out[i].Name)
		if err != nil || len(creds) == 0 {
			continue
		}
		rc := make([]RuntimeCredential, 0, len(creds))
		for _, c := range creds {
			rc = append(rc, RuntimeCredential{Name: c.Name, Username: c.Username, Password: c.Password})
		}
		out[i].RuntimeCredentials = rc
	}
	return out
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

	return NewManagementStateLifecycle(s).ReloadLocked()
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
