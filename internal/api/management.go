package api

import (
	"net/http"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/applyhistory"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/service"
)

var stagedConfigValidator = func(paths []string) []ConfigValidationResult {
	return generatedconfig.NewStagedConfigValidator(generatedconfig.RunFixedConfigValidation).Validate(paths)
}
var serviceActionRunner = func(command []string) ServiceActionResult {
	return service.RunFixedServiceAction(command, service.NewCommandPolicy(NewManagedRuntimeCatalog()), nil)
}
var serviceHealthChecker = func(serviceName string) ServiceHealthResult {
	return service.RunFixedServiceHealthCheck(serviceName, service.NewCommandPolicy(NewManagedRuntimeCatalog()), nil)
}

// Reloader is an optional interface for runtime state reload.
type Reloader interface {
	Reload() error
}

func (s *managementState) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/inbounds", s.handleInbounds)
	mux.HandleFunc("/api/inbounds/", s.handleInboundByName)
	mux.HandleFunc("/api/routing/rules", s.handleRoutingRules)
	mux.HandleFunc("/api/routing/rules/", s.handleRoutingRuleByName)
	mux.HandleFunc("/api/routing/presets", s.handleRoutingPresets)
	mux.HandleFunc("/api/routing/presets/", s.handleRoutingPresetByName)
	mux.HandleFunc("/api/warp", s.handleWarp)
	mux.HandleFunc("/api/client-links/subscription", s.handleClientLinksSubscription)
	mux.HandleFunc("/api/client-links", s.handleClientLinks)
	mux.HandleFunc("/api/firewall", s.handleFirewall)
	mux.HandleFunc("/api/apply/plan", s.handleApplyPlan)
	mux.HandleFunc("/api/apply/history", s.handleApplyHistory)
	mux.HandleFunc("/api/apply", s.handleApply)
}

func (s *managementState) withMutation(fn func(managementstate.Mutation) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(s.mutationLocked())
}

func (s *managementState) mutationLocked() managementstate.Mutation {
	return managementstate.NewMutation(managementstate.MutationTarget{Settings: &s.settings, Inbounds: &s.inbounds, Rules: &s.rules, Warp: &s.warp}, s.saveLocked)
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
		Settings:  s.settings,
		Inbounds:  s.inbounds,
		Rules:     s.rules,
		Warp:      s.warp,
	})
}

func (s *managementState) snapshotLocked() managementSnapshot {
	return NewManagementStateLifecycle(s).SnapshotLocked()
}

func (s *managementState) encryptSnapshot(snapshot *managementSnapshot) {
	managementstate.EncryptSnapshot(snapshot, s.cipher)
}

func (s *managementState) decryptSnapshot(snapshot *managementSnapshot) {
	managementstate.DecryptSnapshot(snapshot, s.cipher)
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
