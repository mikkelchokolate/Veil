package api

import (
	"encoding/json"
	"net/http"
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

type managementState struct {
	mu       sync.Mutex
	settings Settings
	inbounds []Inbound
	rules    []RoutingRule
	warp     WarpConfig
}

func newManagementState(info ServerInfo) *managementState {
	return &managementState{
		settings: Settings{PanelListen: "127.0.0.1:2096", Stack: "both", Mode: info.Mode},
		inbounds: []Inbound{
			{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true},
			{Name: "hysteria2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true},
		},
		rules: []RoutingRule{
			{Name: "default-direct", Match: "geoip:private", Outbound: "direct", Enabled: true},
		},
		warp: WarpConfig{Enabled: false, Endpoint: "engage.cloudflareclient.com:2408"},
	}
}

func (s *managementState) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/inbounds", s.handleInbounds)
	mux.HandleFunc("/api/routing/rules", s.handleRoutingRules)
	mux.HandleFunc("/api/warp", s.handleWarp)
}

func (s *managementState) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, s.settings)
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
		s.inbounds = append(s.inbounds, inbound)
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, inbound)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *managementState) handleRoutingRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, s.rules)
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
		writeJSON(w, s.warp)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
