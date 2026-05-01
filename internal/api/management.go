package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

type Settings struct {
	PanelListen string `json:"panelListen"`
	Stack       string `json:"stack"`
	Mode        string `json:"mode"`
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

type managementSnapshot struct {
	Settings Settings      `json:"settings"`
	Inbounds []Inbound     `json:"inbounds"`
	Rules    []RoutingRule `json:"routingRules"`
	Warp     WarpConfig    `json:"warp"`
}

type managementState struct {
	mu        sync.Mutex
	statePath string
	settings  Settings
	inbounds  []Inbound
	rules     []RoutingRule
	warp      WarpConfig
}

func newManagementState(info ServerInfo) *managementState {
	state := &managementState{
		statePath: info.StatePath,
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
}

func (s *managementState) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.settings)
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
		writeJSON(w, s.settings)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
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
	snapshot := managementSnapshot{
		Settings: s.settings,
		Inbounds: append([]Inbound(nil), s.inbounds...),
		Rules:    append([]RoutingRule(nil), s.rules...),
		Warp:     s.warp,
	}
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
