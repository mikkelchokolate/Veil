package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/veil-panel/veil/internal/secrets"
)

var stagedConfigValidator = runStagedConfigValidators
var serviceActionRunner = runFixedServiceAction
var serviceHealthChecker = runFixedServiceHealthCheck

// Reloader is an optional interface for runtime state reload.
type Reloader interface {
	Reload() error
}

func newManagementState(info ServerInfo) *managementState {
	keyPath := info.KeyPath
	if keyPath == "" {
		keyPath = "/etc/veil/state.key"
	}
	model := defaultManagementModel(info.Mode)
	state := &managementState{
		statePath: info.StatePath,
		applyRoot: defaultApplyRoot(info.ApplyRoot),
		keyPath:   keyPath,
		settings:  model.Settings,
		inbounds:  model.Inbounds,
		rules:     model.Rules,
		warp:      model.Warp,
	}
	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		log.Printf("error loading encryption key from %s: %v", keyPath, err)
	} else {
		cipher, err := secrets.NewCipher(*key)
		if err != nil {
			log.Printf("error creating cipher: %v", err)
		} else {
			state.cipher = cipher
		}
	}
	if err := state.load(); err != nil {
		log.Printf("error loading management state from %s: %v", info.StatePath, err)
	}
	return state
}

func (s *managementState) register(mux *http.ServeMux) {
	ManagementRoutes{}.Register(mux, s)
}

func stackIncludesProtocol(stack string, protocol string) bool {
	switch stack {
	case "both":
		return true
	case "naive":
		return protocol == "naiveproxy"
	case "hysteria2":
		return protocol == "hysteria2"
	default:
		return false
	}
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func (s *managementState) applyHistoryPathLocked() string {
	return filepath.Join(s.applyRoot, "generated", "veil", "apply-history.json")
}

func (s *managementState) loadApplyHistoryLocked() ([]ApplyHistoryEntry, error) {
	return NewApplyHistoryStore(s.applyHistoryPathLocked(), maxApplyHistoryEntries).Load()
}

func requirePassedValidations(validations []ConfigValidationResult) error {
	for _, validation := range validations {
		if validation.Skipped || !validation.Valid {
			if validation.Error != "" {
				return errors.New(validation.Error)
			}
			return fmt.Errorf("%s validation did not pass", validation.Name)
		}
	}
	return nil
}

func (s *managementState) livePathForStagedConfig(stagedPath string) (string, bool) {
	context := NewManagementApplyContext(s)
	return NewLiveConfigPromotion(s.applyRoot, context.reloadPromotedServicesLocked).LivePathForStagedConfig(stagedPath)
}

func checkServiceHealth(actions []ServiceActionResult) []ServiceHealthResult {
	checks := []ServiceHealthResult{}
	for _, action := range actions {
		if !action.Success || action.Name == "" {
			continue
		}
		checks = append(checks, serviceHealthChecker(action.Name))
	}
	return checks
}

func requireHealthyServices(checks []ServiceHealthResult) error {
	for _, check := range checks {
		if !check.Healthy {
			if check.Error != "" {
				return errors.New(check.Error)
			}
			return fmt.Errorf("%s health check failed", check.Name)
		}
	}
	return nil
}

func requireSuccessfulServiceActions(actions []ServiceActionResult) error {
	for _, action := range actions {
		if !action.Success {
			if action.Error != "" {
				return errors.New(action.Error)
			}
			return fmt.Errorf("%s service action failed", action.Name)
		}
	}
	return nil
}

func (s *managementState) renderManagementConfigsLocked() (map[string]string, error) {
	return s.managementConfigRendererLocked().Render()
}

func (s *managementState) hasRenderSettingsLocked() bool {
	return s.managementConfigRendererLocked().HasRenderSettings()
}

func (s *managementState) renderNaiveConfigLocked(inbound Inbound) (string, error) {
	return s.managementConfigRendererLocked().RenderInbound(inbound)
}

func (s *managementState) renderHysteria2ConfigLocked(inbound Inbound) (string, error) {
	return s.managementConfigRendererLocked().RenderInbound(inbound)
}

func (s *managementState) renderWarpConfigLocked() (string, error) {
	return s.managementConfigRendererLocked().RenderWarp()
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
	EncryptManagementSnapshot(snapshot, s.cipher)
}

func (s *managementState) decryptSnapshot(snapshot *managementSnapshot) {
	DecryptManagementSnapshot(snapshot, s.cipher)
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

func buildFirewallRules(settings Settings, inbounds []Inbound) []firewallRuleResponse {
	return BuildFirewallRuleResponses(settings, inbounds)
}
