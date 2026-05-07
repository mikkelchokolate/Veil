package api

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	"github.com/veil-panel/veil/internal/secrets"
)

type Settings struct {
	PanelListen       string `json:"panelListen"`
	Stack             string `json:"stack"`
	Mode              string `json:"mode"`
	Domain            string `json:"domain,omitempty"`
	Email             string `json:"email,omitempty"`
	NaiveUsername     string `json:"naiveUsername,omitempty"`
	NaivePassword     string `json:"naivePassword,omitempty"`
	Hysteria2Password string `json:"hysteria2Password,omitempty"`
	MasqueradeURL     string `json:"masqueradeURL,omitempty"`
	FallbackRoot      string `json:"fallbackRoot,omitempty"`
}

type ClientProfile struct {
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Enabled  bool   `json:"enabled"`
}

type Inbound struct {
	Name      string          `json:"name"`
	Protocol  string          `json:"protocol"`
	Transport string          `json:"transport"`
	Port      int             `json:"port"`
	Enabled   bool            `json:"enabled"`
	Password  string          `json:"password,omitempty"`
	Profiles  []ClientProfile `json:"profiles,omitempty"`
}

type RoutingRule struct {
	Name     string `json:"name"`
	Match    string `json:"match"`
	Outbound string `json:"outbound"`
	Enabled  bool   `json:"enabled"`
}

type RoutingPreset struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Source      RoutingSource `json:"source"`
	Rules       []RoutingRule `json:"rules"`
}

type RoutingPresetResponse struct {
	ActivePreset string          `json:"activePreset,omitempty"`
	Source       RoutingSource   `json:"source"`
	Rules        []RoutingRule   `json:"rules"`
	Presets      []RoutingPreset `json:"presets,omitempty"`
}

type RoutingSource struct {
	Repository string              `json:"repository,omitempty"`
	Files      []RoutingSourceFile `json:"files,omitempty"`
}

type RoutingSourceFile struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	SHA256URL string `json:"sha256Url,omitempty"`
}

type WarpConfig struct {
	Enabled       bool   `json:"enabled"`
	LicenseKey    string `json:"licenseKey,omitempty"`
	Endpoint      string `json:"endpoint"`
	PrivateKey    string `json:"privateKey,omitempty"`
	LocalAddress  string `json:"localAddress,omitempty"`
	PeerPublicKey string `json:"peerPublicKey,omitempty"`
	Reserved      []int  `json:"reserved,omitempty"`
	SocksListen   string `json:"socksListen,omitempty"`
	SocksPort     int    `json:"socksPort,omitempty"`
	MTU           int    `json:"mtu,omitempty"`
}

type ClientLinksResponse struct {
	SchemaVersion              string       `json:"schemaVersion"`
	Domain                     string       `json:"domain"`
	Stack                      string       `json:"stack"`
	SubscriptionURL            string       `json:"subscriptionUrl"`
	Base64SubscriptionURL      string       `json:"base64SubscriptionUrl"`
	RawSubscriptionURL         string       `json:"rawSubscriptionUrl"`
	DefaultSubscriptionFormat  string       `json:"defaultSubscriptionFormat"`
	Base64SubscriptionFilename string       `json:"base64SubscriptionFilename"`
	RawSubscriptionFilename    string       `json:"rawSubscriptionFilename"`
	SubscriptionContentType    string       `json:"subscriptionContentType"`
	SubscriptionFormats        []string     `json:"subscriptionFormats"`
	Count                      int          `json:"count"`
	Links                      []ClientLink `json:"links"`
}

type ClientLink struct {
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Transport string `json:"transport"`
	Port      int    `json:"port"`
	URI       string `json:"uri"`
}

type ApplyPlanResponse struct {
	Valid   bool     `json:"valid"`
	Errors  []string `json:"errors,omitempty"`
	Configs []string `json:"configs"`
	Actions []string `json:"actions"`
}

type ApplyRequest struct {
	Confirm       bool `json:"confirm"`
	ApplyLive     bool `json:"applyLive"`
	ApplyServices bool `json:"applyServices"`
}

type ApplyResponse struct {
	Applied         bool                     `json:"applied"`
	LiveApplied     bool                     `json:"liveApplied"`
	ServicesApplied bool                     `json:"servicesApplied"`
	RolledBack      bool                     `json:"rolledBack,omitempty"`
	Plan            ApplyPlanResponse        `json:"plan"`
	WrittenFiles    []string                 `json:"writtenFiles"`
	LiveFiles       []string                 `json:"liveFiles,omitempty"`
	BackupFiles     []string                 `json:"backupFiles,omitempty"`
	RollbackFiles   []string                 `json:"rollbackFiles,omitempty"`
	Validations     []ConfigValidationResult `json:"validations,omitempty"`
	ServiceActions  []ServiceActionResult    `json:"serviceActions,omitempty"`
	HealthChecks    []ServiceHealthResult    `json:"healthChecks,omitempty"`
	RollbackActions []ServiceActionResult    `json:"rollbackActions,omitempty"`
}

type ApplyHistoryEntry struct {
	ID              string                   `json:"id"`
	Timestamp       string                   `json:"timestamp"`
	Stage           string                   `json:"stage"`
	Success         bool                     `json:"success"`
	Applied         bool                     `json:"applied"`
	LiveApplied     bool                     `json:"liveApplied"`
	ServicesApplied bool                     `json:"servicesApplied"`
	RolledBack      bool                     `json:"rolledBack,omitempty"`
	Plan            ApplyPlanResponse        `json:"plan"`
	WrittenFiles    []string                 `json:"writtenFiles,omitempty"`
	LiveFiles       []string                 `json:"liveFiles,omitempty"`
	BackupFiles     []string                 `json:"backupFiles,omitempty"`
	RollbackFiles   []string                 `json:"rollbackFiles,omitempty"`
	Validations     []ConfigValidationResult `json:"validations,omitempty"`
	ServiceActions  []ServiceActionResult    `json:"serviceActions,omitempty"`
	HealthChecks    []ServiceHealthResult    `json:"healthChecks,omitempty"`
	RollbackActions []ServiceActionResult    `json:"rollbackActions,omitempty"`
}

type ConfigValidationResult struct {
	Name    string   `json:"name"`
	Config  string   `json:"config"`
	Command []string `json:"command"`
	Valid   bool     `json:"valid"`
	Skipped bool     `json:"skipped,omitempty"`
	Output  string   `json:"output,omitempty"`
	Error   string   `json:"error,omitempty"`
}

var stagedConfigValidator = runStagedConfigValidators
var serviceActionRunner = runFixedServiceAction
var serviceHealthChecker = runFixedServiceHealthCheck

type ServiceActionResult struct {
	Name    string   `json:"name"`
	Command []string `json:"command"`
	Success bool     `json:"success"`
	Output  string   `json:"output,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type ServiceHealthResult struct {
	Name    string   `json:"name"`
	Command []string `json:"command"`
	Healthy bool     `json:"healthy"`
	Output  string   `json:"output,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type livePromotionRecord struct {
	LivePath    string
	BackupPath  string
	HadPrevious bool
}

type managementSnapshot struct {
	Settings      Settings      `json:"settings"`
	Inbounds      []Inbound     `json:"inbounds"`
	Rules         []RoutingRule `json:"routingRules"`
	RoutingPreset string        `json:"routingPreset,omitempty"`
	RoutingSource RoutingSource `json:"routingSource,omitempty"`
	Warp          WarpConfig    `json:"warp"`
}

type managementState struct {
	mu            sync.Mutex
	statePath     string
	applyRoot     string
	keyPath       string
	cipher        *secrets.Cipher
	settings      Settings
	inbounds      []Inbound
	rules         []RoutingRule
	routingPreset string
	routingSource RoutingSource
	warp          WarpConfig
}

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

func (s *managementState) handleSettings(w http.ResponseWriter, r *http.Request) {
	_ = s.withTransaction(func(tx *managementTransaction) error {
		management := tx.Settings()
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, management.Get())
		case http.MethodPut:
			var settings Settings
			if !decodeJSONRequest(w, r, &settings) {
				return nil
			}
			updated, err := management.Update(settings)
			if err != nil {
				writeError(w, err.Error(), http.StatusBadRequest)
				return nil
			}
			writeJSON(w, updated)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPut)
		}
		return nil
	})
}

func (s *managementState) handleInbounds(w http.ResponseWriter, r *http.Request) {
	_ = s.withTransaction(func(tx *managementTransaction) error {
		management := tx.Inbounds()
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, management.List())
		case http.MethodPost:
			var inbound Inbound
			if !decodeJSONRequest(w, r, &inbound) {
				return nil
			}
			created, err := management.Create(inbound)
			if err != nil {
				writeInboundManagementError(w, err)
				return nil
			}
			writeJSONStatus(w, http.StatusCreated, created)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return nil
	})
}

func (s *managementState) handleInboundByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/inbounds/")
	if name == "" || strings.Contains(name, "/") {
		writeNotFound(w)
		return
	}
	_ = s.withTransaction(func(tx *managementTransaction) error {
		management := tx.Inbounds()
		inbound, ok := management.Get(name)
		if !ok {
			writeNotFound(w)
			return nil
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, inbound)
		case http.MethodPut:
			var update Inbound
			if !decodeJSONRequest(w, r, &update) {
				return nil
			}
			updated, err := management.Update(name, update)
			if err != nil {
				writeInboundManagementError(w, err)
				return nil
			}
			writeJSON(w, updated)
		case http.MethodDelete:
			if err := management.Delete(name); err != nil {
				writeInboundManagementError(w, err)
				return nil
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
		}
		return nil
	})
}

func writeInboundManagementError(w http.ResponseWriter, err error) {
	switch err {
	case ErrInboundInvalid:
		writeError(w, "name, protocol, transport, and positive port are required", http.StatusBadRequest)
	case ErrInboundDuplicateName:
		writeError(w, "inbound name already exists", http.StatusConflict)
	case ErrInboundDuplicateTransportPort:
		writeError(w, "inbound transport/port already exists", http.StatusConflict)
	case ErrInboundNotFound:
		writeNotFound(w)
	default:
		writeError(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *managementState) handleRoutingRules(w http.ResponseWriter, r *http.Request) {
	_ = s.withTransaction(func(tx *managementTransaction) error {
		management := tx.RoutingRules()
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, management.List())
		case http.MethodPost:
			var rule RoutingRule
			if !decodeJSONRequest(w, r, &rule) {
				return nil
			}
			created, err := management.Create(rule)
			if err != nil {
				writeRoutingRuleManagementError(w, err)
				return nil
			}
			writeJSONStatus(w, http.StatusCreated, created)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
		return nil
	})
}

func (s *managementState) handleRoutingRuleByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/routing/rules/")
	if name == "" || strings.Contains(name, "/") {
		writeNotFound(w)
		return
	}
	_ = s.withTransaction(func(tx *managementTransaction) error {
		management := tx.RoutingRules()
		rule, ok := management.Get(name)
		if !ok {
			writeNotFound(w)
			return nil
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, rule)
		case http.MethodPut:
			var update RoutingRule
			if !decodeJSONRequest(w, r, &update) {
				return nil
			}
			updated, err := management.Update(name, update)
			if err != nil {
				writeRoutingRuleManagementError(w, err)
				return nil
			}
			writeJSON(w, updated)
		case http.MethodDelete:
			if err := management.Delete(name); err != nil {
				writeRoutingRuleManagementError(w, err)
				return nil
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPut, http.MethodDelete)
		}
		return nil
	})
}

func writeRoutingRuleManagementError(w http.ResponseWriter, err error) {
	switch err {
	case ErrRoutingRuleInvalid:
		writeError(w, "name, match, and outbound are required", http.StatusBadRequest)
	case ErrRoutingRuleDuplicateName:
		writeError(w, "routing rule name already exists", http.StatusConflict)
	case ErrRoutingRuleNotFound:
		writeNotFound(w)
	default:
		writeError(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *managementState) handleRoutingPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, RoutingPresetResponse{ActivePreset: s.routingPreset, Source: s.routingSource, Rules: append([]RoutingRule(nil), s.rules...), Presets: routingPresetProfiles()})
}

func (s *managementState) handleRoutingPresetByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/routing/presets/")
	if name == "" || strings.Contains(name, "/") {
		writeNotFound(w)
		return
	}
	preset, ok := routingPresetByName(name)
	if !ok {
		writeNotFound(w)
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routingPreset = preset.Name
	s.routingSource = preset.Source
	s.rules = append([]RoutingRule(nil), preset.Rules...)
	if err := s.saveLocked(); err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, RoutingPresetResponse{ActivePreset: s.routingPreset, Source: s.routingSource, Rules: append([]RoutingRule(nil), s.rules...)})
}

func (s *managementState) handleWarp(w http.ResponseWriter, r *http.Request) {
	_ = s.withTransaction(func(tx *managementTransaction) error {
		management := tx.Warp()
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, management.Get())
		case http.MethodPut:
			var warp WarpConfig
			if !decodeJSONRequest(w, r, &warp) {
				return nil
			}
			updated, err := management.Update(warp)
			if err != nil {
				writeError(w, err.Error(), http.StatusInternalServerError)
				return nil
			}
			writeJSON(w, updated)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPut)
		}
		return nil
	})
}

func (s *managementState) handleClientLinks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	response, err := BuildClientLinks(s.settings, s.inbounds)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writeJSON(w, response)
}

func (s *managementState) handleClientLinksSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	query := r.URL.Query()
	if err := ValidateClientSubscriptionQuery(query); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	format := query.Get("format")
	s.mu.Lock()
	defer s.mu.Unlock()
	response, err := BuildClientLinks(s.settings, s.inbounds)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	subscription, err := BuildClientSubscription(response, format)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", subscription.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, subscription.Filename))
	_, _ = w.Write([]byte(subscription.Body))
}

func (s *managementState) handleFirewall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	active, _ := firewallStatusReader()
	rules := buildFirewallRules(s.settings, s.inbounds)
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]any{
			"active": active,
			"rules":  rules,
		})
	} else {
		setJSONHeaders(w)
	}
}

func (s *managementState) handleApplyPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan := NewManagementApplyContext(s).buildApplyPlanLocked()
	status := http.StatusOK
	if !plan.Valid {
		status = http.StatusBadRequest
	}
	writeJSONStatus(w, status, plan)
}

func (s *managementState) handleApplyHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	history, err := s.loadApplyHistoryLocked()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	history, err = filterApplyHistory(history, r.URL.Query())
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, history)
}

func (s *managementState) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var req ApplyRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	response, status, err := NewApplyWorkflow(NewManagementApplyContext(s)).RunLocked(req)
	if err != nil {
		writeError(w, err.Error(), status)
		return
	}
	if status != http.StatusOK {
		writeJSONStatus(w, status, response)
		return
	}
	writeJSON(w, response)
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
	return BuildManagementSnapshot(ManagementSnapshotInput{
		Settings:      s.settings,
		Inbounds:      s.inbounds,
		Rules:         s.rules,
		RoutingPreset: s.routingPreset,
		RoutingSource: s.routingSource,
		Warp:          s.warp,
	})
}

func (s *managementState) encryptSnapshot(snapshot *managementSnapshot) {
	EncryptManagementSnapshot(snapshot, s.cipher)
}

func (s *managementState) decryptSnapshot(snapshot *managementSnapshot) {
	DecryptManagementSnapshot(snapshot, s.cipher)
}

func (s *managementState) load() error {
	snapshot, ok, err := NewStateStore(s.statePath, s.cipher).Load()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	ApplyManagementSnapshot(s, snapshot)
	return nil
}

// Reload re-reads the management state and encryption key from disk.
// It locks the state mutex during the reload. Returns an error if the
// state file or key file cannot be read, but leaves the previous state
// intact on failure.
func (s *managementState) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Reload encryption key
	if s.keyPath != "" {
		key, err := secrets.LoadOrCreateKey(s.keyPath)
		if err != nil {
			return fmt.Errorf("reload key: %w", err)
		}
		cipher, err := secrets.NewCipher(*key)
		if err != nil {
			return fmt.Errorf("reload cipher: %w", err)
		}
		s.cipher = cipher
	}

	// Reload state from disk
	if s.statePath != "" {
		if err := s.load(); err != nil {
			return fmt.Errorf("reload state: %w", err)
		}
	}

	return nil
}

func (s *managementState) saveLocked() error {
	return NewStateStore(s.statePath, s.cipher).Save(s.snapshotLocked())
}

func buildFirewallRules(settings Settings, inbounds []Inbound) []firewallRuleResponse {
	return BuildFirewallRuleResponses(settings, inbounds)
}
