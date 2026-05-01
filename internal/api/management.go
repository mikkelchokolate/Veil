package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/veil-panel/veil/internal/renderer"
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

type Inbound struct {
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Transport string `json:"transport"`
	Port      int    `json:"port"`
	Enabled   bool   `json:"enabled"`
}

type RoutingRule struct {
	Name     string `json:"name"`
	Match    string `json:"match"`
	Outbound string `json:"outbound"`
	Enabled  bool   `json:"enabled"`
}

type WarpConfig struct {
	Enabled    bool   `json:"enabled"`
	LicenseKey string `json:"licenseKey,omitempty"`
	Endpoint   string `json:"endpoint"`
}

type ApplyPlanResponse struct {
	Valid   bool     `json:"valid"`
	Errors  []string `json:"errors,omitempty"`
	Configs []string `json:"configs"`
	Actions []string `json:"actions"`
}

type ApplyRequest struct {
	Confirm bool `json:"confirm"`
}

type ApplyResponse struct {
	Applied      bool              `json:"applied"`
	Plan         ApplyPlanResponse `json:"plan"`
	WrittenFiles []string          `json:"writtenFiles"`
}

type managementSnapshot struct {
	Settings Settings      `json:"settings"`
	Inbounds []Inbound     `json:"inbounds"`
	Rules    []RoutingRule `json:"routingRules"`
	Warp     WarpConfig    `json:"warp"`
}

type managementState struct {
	mu        sync.Mutex
	statePath string
	applyRoot string
	settings  Settings
	inbounds  []Inbound
	rules     []RoutingRule
	warp      WarpConfig
}

func newManagementState(info ServerInfo) *managementState {
	state := &managementState{
		statePath: info.StatePath,
		applyRoot: defaultApplyRoot(info.ApplyRoot),
		settings:  Settings{PanelListen: "127.0.0.1:2096", Stack: "both", Mode: info.Mode},
		inbounds: []Inbound{
			{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
			{Name: "hysteria2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
		},
		rules: []RoutingRule{
			{Name: "default-direct", Match: "geoip:private", Outbound: "direct", Enabled: true},
		},
		warp: WarpConfig{Enabled: false, Endpoint: "engage.cloudflareclient.com:2408"},
	}
	_ = state.load()
	return state
}

func (s *managementState) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/inbounds", s.handleInbounds)
	mux.HandleFunc("/api/routing/rules", s.handleRoutingRules)
	mux.HandleFunc("/api/warp", s.handleWarp)
	mux.HandleFunc("/api/apply/plan", s.handleApplyPlan)
	mux.HandleFunc("/api/apply", s.handleApply)
}

func (s *managementState) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, redactedSettings(s.settings))
	case http.MethodPut:
		var settings Settings
		if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if settings.PanelListen == "" || settings.Stack == "" || settings.Mode == "" {
			http.Error(w, "panelListen, stack, and mode are required", http.StatusBadRequest)
			return
		}
		s.settings = settings
		if err := s.saveLocked(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, redactedSettings(s.settings))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func redactedSettings(settings Settings) Settings {
	redacted := settings
	if redacted.NaivePassword != "" {
		redacted.NaivePassword = "[REDACTED]"
	}
	if redacted.Hysteria2Password != "" {
		redacted.Hysteria2Password = "[REDACTED]"
	}
	return redacted
}

func (s *managementState) handleInbounds(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.inbounds)
	case http.MethodPost:
		var inbound Inbound
		if err := json.NewDecoder(r.Body).Decode(&inbound); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if inbound.Name == "" || inbound.Protocol == "" || inbound.Transport == "" || inbound.Port <= 0 {
			http.Error(w, "name, protocol, transport, and positive port are required", http.StatusBadRequest)
			return
		}
		if s.hasInboundTransportPort(inbound.Transport, inbound.Port) {
			http.Error(w, "inbound transport/port already exists", http.StatusConflict)
			return
		}
		s.inbounds = append(s.inbounds, inbound)
		if err := s.saveLocked(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, inbound)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *managementState) handleRoutingRules(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.rules)
	case http.MethodPost:
		var rule RoutingRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if rule.Name == "" || rule.Match == "" || rule.Outbound == "" {
			http.Error(w, "name, match, and outbound are required", http.StatusBadRequest)
			return
		}
		s.rules = append(s.rules, rule)
		if err := s.saveLocked(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, rule)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *managementState) handleWarp(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.warp)
	case http.MethodPut:
		var warp WarpConfig
		if err := json.NewDecoder(r.Body).Decode(&warp); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if warp.Endpoint == "" {
			warp.Endpoint = "engage.cloudflareclient.com:2408"
		}
		s.warp = warp
		if err := s.saveLocked(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, s.warp)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *managementState) handleApplyPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan := s.buildApplyPlanLocked()
	if !plan.Valid {
		w.WriteHeader(http.StatusBadRequest)
	}
	writeJSON(w, plan)
}

func (s *managementState) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req ApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	plan := s.buildApplyPlanLocked()
	if !plan.Valid {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, ApplyResponse{Applied: false, Plan: plan})
		return
	}
	if !req.Confirm {
		http.Error(w, "confirm=true is required to write staged apply files", http.StatusBadRequest)
		return
	}
	written, err := s.writeApplyStageLocked(plan)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, ApplyResponse{Applied: true, Plan: plan, WrittenFiles: written})
}

func (s *managementState) buildApplyPlanLocked() ApplyPlanResponse {
	plan := ApplyPlanResponse{
		Valid:   true,
		Configs: []string{},
		Actions: []string{"validate management state"},
	}
	if s.settings.Stack != "both" && s.settings.Stack != "naive" && s.settings.Stack != "hysteria2" {
		plan.Errors = append(plan.Errors, "unsupported stack: "+s.settings.Stack)
	}
	seen := map[string]bool{}
	for _, inbound := range s.inbounds {
		if !inbound.Enabled || !stackIncludesProtocol(s.settings.Stack, inbound.Protocol) {
			continue
		}
		if inbound.Name == "" || inbound.Protocol == "" || inbound.Transport == "" {
			plan.Errors = append(plan.Errors, "enabled inbounds require name, protocol, and transport")
		}
		if inbound.Port <= 0 {
			plan.Errors = append(plan.Errors, "enabled inbounds require a positive port")
		}
		key := inbound.Transport + ":" + fmt.Sprint(inbound.Port)
		if seen[key] {
			plan.Errors = append(plan.Errors, "duplicate enabled inbound transport/port")
		}
		seen[key] = true
		switch inbound.Protocol {
		case "naiveproxy":
			plan.Configs = appendUnique(plan.Configs, "/etc/veil/generated/caddy/Caddyfile")
			plan.Actions = appendUnique(plan.Actions, "reload veil-naive.service")
			if s.hasRenderSettingsLocked() {
				if _, err := s.renderNaiveConfigLocked(inbound); err != nil {
					plan.Errors = append(plan.Errors, err.Error())
				}
			}
		case "hysteria2":
			plan.Configs = appendUnique(plan.Configs, "/etc/veil/generated/hysteria2/server.yaml")
			plan.Actions = appendUnique(plan.Actions, "reload veil-hysteria2.service")
			if s.hasRenderSettingsLocked() {
				if _, err := s.renderHysteria2ConfigLocked(inbound); err != nil {
					plan.Errors = append(plan.Errors, err.Error())
				}
			}
		default:
			if inbound.Protocol != "" {
				plan.Errors = append(plan.Errors, "unsupported inbound protocol: "+inbound.Protocol)
			}
		}
	}
	if len(plan.Configs) > 0 {
		plan.Actions = append([]string{"validate management state", "stage generated configs"}, plan.Actions[1:]...)
	}
	if len(plan.Errors) > 0 {
		plan.Valid = false
	}
	return plan
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

func (s *managementState) writeApplyStageLocked(plan ApplyPlanResponse) ([]string, error) {
	stageDir := filepath.Join(s.applyRoot, "generated", "veil")
	planPath := filepath.Join(stageDir, "apply-plan.json")
	statePath := filepath.Join(stageDir, "management-state.json")
	planBody, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeAtomicFile(planPath, append(planBody, '\n'), 0o600); err != nil {
		return nil, err
	}
	snapshotBody, err := json.MarshalIndent(s.snapshotLocked(), "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeAtomicFile(statePath, append(snapshotBody, '\n'), 0o600); err != nil {
		return nil, err
	}
	written := []string{planPath, statePath}
	rendered, err := s.renderManagementConfigsLocked()
	if err != nil {
		return nil, err
	}
	for path, body := range rendered {
		if err := writeAtomicFile(path, []byte(body), 0o600); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}

func (s *managementState) renderManagementConfigsLocked() (map[string]string, error) {
	configs := map[string]string{}
	if !s.hasRenderSettingsLocked() {
		return configs, nil
	}
	for _, inbound := range s.inbounds {
		if !inbound.Enabled || !stackIncludesProtocol(s.settings.Stack, inbound.Protocol) {
			continue
		}
		switch inbound.Protocol {
		case "naiveproxy":
			body, err := s.renderNaiveConfigLocked(inbound)
			if err != nil {
				return nil, err
			}
			configs[filepath.Join(s.applyRoot, "generated", "caddy", "Caddyfile")] = body
		case "hysteria2":
			body, err := s.renderHysteria2ConfigLocked(inbound)
			if err != nil {
				return nil, err
			}
			configs[filepath.Join(s.applyRoot, "generated", "hysteria2", "server.yaml")] = body
		}
	}
	return configs, nil
}

func (s *managementState) hasRenderSettingsLocked() bool {
	return s.settings.Domain != "" || s.settings.Email != "" || s.settings.NaiveUsername != "" || s.settings.NaivePassword != "" || s.settings.Hysteria2Password != "" || s.settings.MasqueradeURL != "" || s.settings.FallbackRoot != ""
}

func (s *managementState) renderNaiveConfigLocked(inbound Inbound) (string, error) {
	return renderer.RenderNaiveCaddyfile(renderer.NaiveConfig{
		Domain:       s.settings.Domain,
		Email:        s.settings.Email,
		ListenPort:   inbound.Port,
		Username:     s.settings.NaiveUsername,
		Password:     s.settings.NaivePassword,
		FallbackRoot: s.settings.FallbackRoot,
	})
}

func (s *managementState) renderHysteria2ConfigLocked(inbound Inbound) (string, error) {
	return renderer.RenderHysteria2(renderer.Hysteria2Config{
		ListenPort:    inbound.Port,
		Domain:        s.settings.Domain,
		Password:      s.settings.Hysteria2Password,
		MasqueradeURL: s.settings.MasqueradeURL,
	})
}

func (s *managementState) snapshotLocked() managementSnapshot {
	return managementSnapshot{
		Settings: s.settings,
		Inbounds: append([]Inbound(nil), s.inbounds...),
		Rules:    append([]RoutingRule(nil), s.rules...),
		Warp:     s.warp,
	}
}

func defaultApplyRoot(root string) string {
	if root != "" {
		return root
	}
	return "/etc/veil"
}

func writeAtomicFile(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *managementState) hasInboundTransportPort(transport string, port int) bool {
	for _, existing := range s.inbounds {
		if existing.Transport == transport && existing.Port == port {
			return true
		}
	}
	return false
}

func (s *managementState) load() error {
	if s.statePath == "" {
		return nil
	}
	body, err := os.ReadFile(s.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var snapshot managementSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return err
	}
	if snapshot.Settings.PanelListen != "" {
		s.settings = snapshot.Settings
	}
	if snapshot.Inbounds != nil {
		s.inbounds = snapshot.Inbounds
	}
	if snapshot.Rules != nil {
		s.rules = snapshot.Rules
	}
	if snapshot.Warp.Endpoint != "" || snapshot.Warp.Enabled || snapshot.Warp.LicenseKey != "" {
		s.warp = snapshot.Warp
	}
	return nil
}

func (s *managementState) saveLocked() error {
	if s.statePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.statePath), 0o700); err != nil {
		return err
	}
	snapshot := s.snapshotLocked()
	body, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.statePath + ".tmp"
	if err := os.WriteFile(tmp, append(body, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.statePath)
}
