package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/veil-panel/veil/internal/renderer"
)

const maxApplyHistoryEntries = 100

var allowedApplyHistoryStages = map[string]bool{
	"staged":     true,
	"live":       true,
	"services":   true,
	"rollback":   true,
	"validation": true,
}

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
	settings      Settings
	inbounds      []Inbound
	rules         []RoutingRule
	routingPreset string
	routingSource RoutingSource
	warp          WarpConfig
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
	mux.HandleFunc("/api/inbounds/", s.handleInboundByName)
	mux.HandleFunc("/api/routing/rules", s.handleRoutingRules)
	mux.HandleFunc("/api/routing/rules/", s.handleRoutingRuleByName)
	mux.HandleFunc("/api/routing/presets", s.handleRoutingPresets)
	mux.HandleFunc("/api/routing/presets/", s.handleRoutingPresetByName)
	mux.HandleFunc("/api/warp", s.handleWarp)
	mux.HandleFunc("/api/client-links/subscription", s.handleClientLinksSubscription)
	mux.HandleFunc("/api/client-links", s.handleClientLinks)
	mux.HandleFunc("/api/apply/plan", s.handleApplyPlan)
	mux.HandleFunc("/api/apply/history", s.handleApplyHistory)
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
		if !decodeJSONRequest(w, r, &settings) {
			return
		}
		if settings.PanelListen == "" || settings.Stack == "" || settings.Mode == "" {
			writeError(w, "panelListen, stack, and mode are required", http.StatusBadRequest)
			return
		}
		if settings.NaivePassword == "[REDACTED]" {
			settings.NaivePassword = s.settings.NaivePassword
		}
		if settings.Hysteria2Password == "[REDACTED]" {
			settings.Hysteria2Password = s.settings.Hysteria2Password
		}
		s.settings = settings
		if err := s.saveLocked(); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
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

func redactedWarp(warp WarpConfig) WarpConfig {
	redacted := warp
	if redacted.PrivateKey != "" {
		redacted.PrivateKey = "[REDACTED]"
	}
	if redacted.LicenseKey != "" {
		redacted.LicenseKey = "[REDACTED]"
	}
	return redacted
}

func setWarpDefaults(warp *WarpConfig) {
	if warp.Endpoint == "" {
		warp.Endpoint = "engage.cloudflareclient.com:2408"
	}
	if warp.SocksListen == "" {
		warp.SocksListen = "127.0.0.1"
	}
	if warp.SocksPort == 0 {
		warp.SocksPort = 40000
	}
	if warp.MTU == 0 {
		warp.MTU = 1280
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
		if !decodeJSONRequest(w, r, &inbound) {
			return
		}
		if inbound.Name == "" || inbound.Protocol == "" || inbound.Transport == "" || inbound.Port <= 0 {
			writeError(w, "name, protocol, transport, and positive port are required", http.StatusBadRequest)
			return
		}
		if s.inboundIndex(inbound.Name) >= 0 {
			writeError(w, "inbound name already exists", http.StatusConflict)
			return
		}
		if s.hasInboundTransportPort(inbound.Transport, inbound.Port) {
			writeError(w, "inbound transport/port already exists", http.StatusConflict)
			return
		}
		s.inbounds = append(s.inbounds, inbound)
		if err := s.saveLocked(); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, inbound)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *managementState) handleInboundByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/inbounds/")
	if name == "" || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.inboundIndex(name)
	if idx < 0 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var update Inbound
		if !decodeJSONRequest(w, r, &update) {
			return
		}
		if update.Protocol == "" || update.Transport == "" || update.Port <= 0 {
			writeError(w, "protocol, transport, and positive port are required", http.StatusBadRequest)
			return
		}
		if s.hasInboundTransportPortExcept(update.Transport, update.Port, idx) {
			writeError(w, "inbound transport/port already exists", http.StatusConflict)
			return
		}
		update.Name = name
		s.inbounds[idx] = update
		if err := s.saveLocked(); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, update)
	case http.MethodDelete:
		s.inbounds = append(s.inbounds[:idx], s.inbounds[idx+1:]...)
		if err := s.saveLocked(); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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
		if !decodeJSONRequest(w, r, &rule) {
			return
		}
		if rule.Name == "" || rule.Match == "" || rule.Outbound == "" {
			writeError(w, "name, match, and outbound are required", http.StatusBadRequest)
			return
		}
		if s.routingRuleIndex(rule.Name) >= 0 {
			writeError(w, "routing rule name already exists", http.StatusConflict)
			return
		}
		s.rules = append(s.rules, rule)
		if err := s.saveLocked(); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, rule)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *managementState) handleRoutingRuleByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/routing/rules/")
	if name == "" || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.routingRuleIndex(name)
	if idx < 0 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var update RoutingRule
		if !decodeJSONRequest(w, r, &update) {
			return
		}
		if update.Match == "" || update.Outbound == "" {
			writeError(w, "match and outbound are required", http.StatusBadRequest)
			return
		}
		update.Name = name
		s.rules[idx] = update
		if err := s.saveLocked(); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, update)
	case http.MethodDelete:
		s.rules = append(s.rules[:idx], s.rules[idx+1:]...)
		if err := s.saveLocked(); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *managementState) routingRuleIndex(name string) int {
	for idx, rule := range s.rules {
		if rule.Name == name {
			return idx
		}
	}
	return -1
}

const routingRulesRepository = "https://github.com/runetfreedom/russia-v2ray-rules-dat"

var routeDatDownloader = downloadRouteDat

func routeDatSource() RoutingSource {
	return RoutingSource{
		Repository: routingRulesRepository,
		Files: []RoutingSourceFile{
			{Name: "geoip.dat", URL: "https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geoip.dat", SHA256URL: "https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geoip.dat.sha256sum"},
			{Name: "geosite.dat", URL: "https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geosite.dat", SHA256URL: "https://github.com/runetfreedom/russia-v2ray-rules-dat/releases/latest/download/geosite.dat.sha256sum"},
		},
	}
}

func routingPresetProfiles() []RoutingPreset {
	source := routeDatSource()
	return []RoutingPreset{
		{
			Name:        "all",
			Description: "Route all traffic through WARP.",
			Rules:       []RoutingRule{{Name: "preset-all-through-warp", Match: "all", Outbound: "warp", Enabled: true}},
		},
		{
			Name:        "all-except-Russia",
			Description: "Route Russian geo/site categories direct and everything else through WARP.",
			Source:      source,
			Rules: []RoutingRule{
				{Name: "preset-all-except-russia-private", Match: "geoip:private", Outbound: "direct", Enabled: true},
				{Name: "preset-all-except-russia-geoip", Match: "geoip:ru", Outbound: "direct", Enabled: true},
				{Name: "preset-all-except-russia-geosite", Match: "geosite:category-ru", Outbound: "direct", Enabled: true},
				{Name: "preset-all-except-russia-rest", Match: "all", Outbound: "warp", Enabled: true},
			},
		},
		{
			Name:        "RU-blocked",
			Description: "Route domains and IPs blocked in Russia through WARP; leave everything else direct.",
			Source:      source,
			Rules: []RoutingRule{
				{Name: "preset-ru-blocked-geoip", Match: "geoip:ru-blocked", Outbound: "warp", Enabled: true},
				{Name: "preset-ru-blocked-geosite", Match: "geosite:ru-blocked", Outbound: "warp", Enabled: true},
			},
		},
	}
}

func routingPresetByName(name string) (RoutingPreset, bool) {
	for _, preset := range routingPresetProfiles() {
		if preset.Name == name {
			return preset, true
		}
	}
	return RoutingPreset{}, false
}

func (s *managementState) handleRoutingPresets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, RoutingPresetResponse{ActivePreset: s.routingPreset, Source: s.routingSource, Rules: append([]RoutingRule(nil), s.rules...), Presets: routingPresetProfiles()})
}

func (s *managementState) handleRoutingPresetByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/routing/presets/")
	if name == "" || strings.Contains(name, "/") {
		http.NotFound(w, r)
		return
	}
	preset, ok := routingPresetByName(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
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

func downloadRouteDat(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("download %s returned %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func fetchVerifiedRouteDatFile(file RoutingSourceFile) ([]byte, error) {
	body, err := routeDatDownloader(file.URL)
	if err != nil {
		return nil, err
	}
	if file.SHA256URL == "" {
		return body, nil
	}
	checksumBody, err := routeDatDownloader(file.SHA256URL)
	if err != nil {
		return nil, err
	}
	if err := verifyRouteDatChecksum(file.Name, body, string(checksumBody)); err != nil {
		return nil, err
	}
	return body, nil
}

func verifyRouteDatChecksum(name string, body []byte, checksumText string) error {
	fields := strings.Fields(checksumText)
	if len(fields) == 0 {
		return fmt.Errorf("checksum for %s is empty", name)
	}
	expected := ""
	for i := 0; i < len(fields); i++ {
		if fields[i] == name && i > 0 {
			expected = fields[i-1]
			break
		}
	}
	if expected == "" {
		expected = fields[0]
	}
	expected = strings.TrimPrefix(strings.ToLower(expected), "sha256:")
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("invalid checksum for %s", name)
	}
	actual := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(actual[:]), expected) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}

func (s *managementState) handleWarp(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, redactedWarp(s.warp))
	case http.MethodPut:
		var warp WarpConfig
		if !decodeJSONRequest(w, r, &warp) {
			return
		}
		if warp.LicenseKey == "[REDACTED]" {
			warp.LicenseKey = s.warp.LicenseKey
		}
		if warp.PrivateKey == "[REDACTED]" {
			warp.PrivateKey = s.warp.PrivateKey
		}
		setWarpDefaults(&warp)
		s.warp = warp
		if err := s.saveLocked(); err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, redactedWarp(s.warp))
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *managementState) handleClientLinks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	response, err := buildClientLinks(s.settings, s.inbounds)
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	format := r.URL.Query().Get("format")
	if format != "" && format != "base64" && format != "raw" {
		writeError(w, "format must be base64 or raw", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	response, err := buildClientLinks(s.settings, s.inbounds)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	uris := make([]string, 0, len(response.Links))
	for _, link := range response.Links {
		uris = append(uris, link.URI)
	}
	payload := strings.Join(uris, "\n") + "\n"
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch format {
	case "", "base64":
		encoded := base64.StdEncoding.EncodeToString([]byte(payload))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="veil-subscription.txt"`)
		_, _ = w.Write([]byte(encoded + "\n"))
	case "raw":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="veil-subscription-raw.txt"`)
		_, _ = w.Write([]byte(payload))
	default:
		writeError(w, "format must be base64 or raw", http.StatusBadRequest)
	}
}

func buildClientLinks(settings Settings, inbounds []Inbound) (ClientLinksResponse, error) {
	if strings.TrimSpace(settings.Domain) == "" {
		return ClientLinksResponse{}, errors.New("domain is required to build client links")
	}
	response := ClientLinksResponse{
		SchemaVersion:              "v1",
		Domain:                     settings.Domain,
		Stack:                      settings.Stack,
		SubscriptionURL:            "/api/client-links/subscription",
		Base64SubscriptionURL:      "/api/client-links/subscription?format=base64",
		RawSubscriptionURL:         "/api/client-links/subscription?format=raw",
		DefaultSubscriptionFormat:  "base64",
		Base64SubscriptionFilename: "veil-subscription.txt",
		RawSubscriptionFilename:    "veil-subscription-raw.txt",
		SubscriptionContentType:    "text/plain; charset=utf-8",
		SubscriptionFormats:        []string{"base64", "raw"},
	}
	for _, inbound := range inbounds {
		if !inbound.Enabled || !stackAllowsProtocol(settings.Stack, inbound.Protocol) {
			continue
		}
		link := ClientLink{Name: inbound.Name, Protocol: inbound.Protocol, Transport: inbound.Transport, Port: inbound.Port}
		switch inbound.Protocol {
		case "naiveproxy":
			if settings.NaiveUsername == "" || settings.NaivePassword == "" {
				return ClientLinksResponse{}, errors.New("naive username and password are required to build client links")
			}
			link.URI = naiveClientURI(settings.Domain, inbound.Port, settings.NaiveUsername, settings.NaivePassword)
		case "hysteria2":
			if settings.Hysteria2Password == "" {
				return ClientLinksResponse{}, errors.New("hysteria2 password is required to build client links")
			}
			link.URI = hysteria2ClientURI(settings.Domain, inbound.Port, settings.Hysteria2Password, inbound.Name)
		default:
			continue
		}
		response.Links = append(response.Links, link)
	}
	if len(response.Links) == 0 {
		return ClientLinksResponse{}, errors.New("no enabled client links are available")
	}
	response.Count = len(response.Links)
	return response, nil
}

func stackAllowsProtocol(stack string, protocol string) bool {
	switch stack {
	case "naive":
		return protocol == "naiveproxy"
	case "hysteria2":
		return protocol == "hysteria2"
	default:
		return true
	}
}

func naiveClientURI(domain string, port int, username string, password string) string {
	userinfo := url.UserPassword(username, password).String()
	return fmt.Sprintf("https://%s@%s:%d", userinfo, domain, port)
}

func hysteria2ClientURI(domain string, port int, password string, name string) string {
	query := url.Values{}
	query.Set("sni", domain)
	fragment := url.QueryEscape(name)
	return fmt.Sprintf("hysteria2://%s@%s:%d/?%s#%s", url.QueryEscape(password), domain, port, query.Encode(), fragment)
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

func (s *managementState) handleApplyHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
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
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req ApplyRequest
	if !decodeJSONRequest(w, r, &req) {
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
		writeError(w, "confirm=true is required to write staged apply files", http.StatusBadRequest)
		return
	}
	if req.ApplyServices && !req.ApplyLive {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, ApplyResponse{Applied: false, Plan: plan})
		return
	}
	written, validations, renderedPaths, err := s.writeApplyStageLocked(plan)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response := ApplyResponse{Applied: true, Plan: plan, WrittenFiles: written, Validations: validations}
	if req.ApplyLive {
		if err := requirePassedValidations(validations); err != nil {
			_ = s.appendApplyHistoryLocked("validation", false, response)
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, response)
			return
		}
		liveFiles, backupFiles, promotionRecords, err := s.promoteStagedConfigsLocked(renderedPaths)
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		response.LiveApplied = true
		response.LiveFiles = liveFiles
		response.BackupFiles = backupFiles
		if req.ApplyServices {
			serviceActions := s.reloadPromotedServicesLocked(liveFiles)
			response.ServiceActions = serviceActions
			if err := requireSuccessfulServiceActions(serviceActions); err != nil {
				rollbackFiles, rollbackActions := s.rollbackPromotedConfigsLocked(promotionRecords, liveFiles)
				response.RolledBack = len(rollbackFiles) > 0
				response.RollbackFiles = rollbackFiles
				response.RollbackActions = rollbackActions
				_ = s.appendApplyHistoryLocked("rollback", false, response)
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, response)
				return
			}
			healthChecks := checkServiceHealth(serviceActions)
			response.HealthChecks = healthChecks
			if err := requireHealthyServices(healthChecks); err != nil {
				rollbackFiles, rollbackActions := s.rollbackPromotedConfigsLocked(promotionRecords, liveFiles)
				response.RolledBack = len(rollbackFiles) > 0
				response.RollbackFiles = rollbackFiles
				response.RollbackActions = rollbackActions
				_ = s.appendApplyHistoryLocked("rollback", false, response)
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, response)
				return
			}
			response.ServicesApplied = len(serviceActions) > 0
		}
	}
	_ = s.appendApplyHistoryLocked(applyHistoryStage(response), true, response)
	writeJSON(w, response)
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
	if s.warp.Enabled {
		plan.Configs = appendUnique(plan.Configs, "/etc/veil/generated/sing-box/warp.json")
		plan.Actions = appendUnique(plan.Actions, "reload veil-warp.service")
		if _, err := s.renderWarpConfigLocked(); err != nil {
			plan.Errors = append(plan.Errors, err.Error())
		}
	}
	for _, rule := range s.rules {
		if !rule.Enabled {
			continue
		}
		if rule.Name == "" || rule.Match == "" || rule.Outbound == "" {
			plan.Errors = append(plan.Errors, "enabled routing rules require name, match, and outbound")
			continue
		}
		switch rule.Outbound {
		case "direct":
		case "warp":
			if !s.warp.Enabled {
				plan.Errors = append(plan.Errors, "routing rule "+rule.Name+" requires WARP to be enabled")
			}
		default:
			plan.Errors = append(plan.Errors, "unsupported routing outbound: "+rule.Outbound)
		}
	}
	for _, file := range s.routingSource.Files {
		if file.Name == "" || file.URL == "" {
			plan.Errors = append(plan.Errors, "routing source files require name and URL")
			continue
		}
		plan.Configs = appendUnique(plan.Configs, "/etc/veil/generated/rules/"+file.Name)
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

func (s *managementState) applyHistoryPathLocked() string {
	return filepath.Join(s.applyRoot, "generated", "veil", "apply-history.json")
}

func (s *managementState) loadApplyHistoryLocked() ([]ApplyHistoryEntry, error) {
	path := s.applyHistoryPathLocked()
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []ApplyHistoryEntry{}, nil
		}
		return nil, err
	}
	var history []ApplyHistoryEntry
	if err := json.Unmarshal(body, &history); err != nil {
		return nil, err
	}
	return history, nil
}

func filterApplyHistory(history []ApplyHistoryEntry, values map[string][]string) ([]ApplyHistoryEntry, error) {
	stage := firstQueryValue(values, "stage")
	successText := firstQueryValue(values, "success")
	limitText := firstQueryValue(values, "limit")
	var successFilter *bool
	if stage != "" && !allowedApplyHistoryStages[stage] {
		return nil, fmt.Errorf("invalid stage filter: %s", stage)
	}
	if successText != "" {
		parsed, err := strconv.ParseBool(successText)
		if err != nil {
			return nil, fmt.Errorf("invalid success filter: %s", successText)
		}
		successFilter = &parsed
	}
	limit := 0
	if limitText != "" {
		parsed, err := strconv.Atoi(limitText)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("invalid limit: %s", limitText)
		}
		limit = parsed
	}
	filtered := make([]ApplyHistoryEntry, 0, len(history))
	for _, entry := range history {
		if stage != "" && entry.Stage != stage {
			continue
		}
		if successFilter != nil && entry.Success != *successFilter {
			continue
		}
		filtered = append(filtered, entry)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

func firstQueryValue(values map[string][]string, key string) string {
	if values == nil || len(values[key]) == 0 {
		return ""
	}
	return values[key][0]
}

func (s *managementState) appendApplyHistoryLocked(stage string, success bool, response ApplyResponse) error {
	history, err := s.loadApplyHistoryLocked()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	entry := ApplyHistoryEntry{
		ID:              now.Format("20060102T150405.000000000Z"),
		Timestamp:       now.Format(time.RFC3339Nano),
		Stage:           stage,
		Success:         success,
		Applied:         response.Applied,
		LiveApplied:     response.LiveApplied,
		ServicesApplied: response.ServicesApplied,
		RolledBack:      response.RolledBack,
		Plan:            response.Plan,
		WrittenFiles:    append([]string(nil), response.WrittenFiles...),
		LiveFiles:       append([]string(nil), response.LiveFiles...),
		BackupFiles:     append([]string(nil), response.BackupFiles...),
		RollbackFiles:   append([]string(nil), response.RollbackFiles...),
		Validations:     append([]ConfigValidationResult(nil), response.Validations...),
		ServiceActions:  append([]ServiceActionResult(nil), response.ServiceActions...),
		HealthChecks:    append([]ServiceHealthResult(nil), response.HealthChecks...),
		RollbackActions: append([]ServiceActionResult(nil), response.RollbackActions...),
	}
	history = append([]ApplyHistoryEntry{entry}, history...)
	if len(history) > maxApplyHistoryEntries {
		history = history[:maxApplyHistoryEntries]
	}
	body, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomicFile(s.applyHistoryPathLocked(), append(body, '\n'), 0o600)
}

func applyHistoryStage(response ApplyResponse) string {
	switch {
	case response.RolledBack:
		return "rollback"
	case response.ServicesApplied:
		return "services"
	case response.LiveApplied:
		return "live"
	default:
		return "staged"
	}
}

func (s *managementState) writeApplyStageLocked(plan ApplyPlanResponse) ([]string, []ConfigValidationResult, []string, error) {
	stageDir := filepath.Join(s.applyRoot, "generated", "veil")
	planPath := filepath.Join(stageDir, "apply-plan.json")
	statePath := filepath.Join(stageDir, "management-state.json")
	planBody, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return nil, nil, nil, err
	}
	if err := writeAtomicFile(planPath, append(planBody, '\n'), 0o600); err != nil {
		return nil, nil, nil, err
	}
	snapshotBody, err := json.MarshalIndent(s.snapshotLocked(), "", "  ")
	if err != nil {
		return nil, nil, nil, err
	}
	if err := writeAtomicFile(statePath, append(snapshotBody, '\n'), 0o600); err != nil {
		return nil, nil, nil, err
	}
	written := []string{planPath, statePath}
	rendered, err := s.renderManagementConfigsLocked()
	if err != nil {
		return nil, nil, nil, err
	}
	renderedPaths := make([]string, 0, len(rendered))
	for path := range rendered {
		renderedPaths = append(renderedPaths, path)
	}
	sort.Strings(renderedPaths)
	for _, path := range renderedPaths {
		if err := writeAtomicFile(path, []byte(rendered[path]), 0o600); err != nil {
			return nil, nil, nil, err
		}
		written = append(written, path)
	}
	for _, file := range s.routingSource.Files {
		body, err := fetchVerifiedRouteDatFile(file)
		if err != nil {
			return nil, nil, nil, err
		}
		path := filepath.Join(s.applyRoot, "generated", "rules", file.Name)
		if err := writeAtomicFile(path, body, 0o600); err != nil {
			return nil, nil, nil, err
		}
		written = append(written, path)
	}
	validations := stagedConfigValidator(renderedPaths)
	return written, validations, renderedPaths, nil
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

func (s *managementState) promoteStagedConfigsLocked(stagedPaths []string) ([]string, []string, []livePromotionRecord, error) {
	liveFiles := []string{}
	backupFiles := []string{}
	records := []livePromotionRecord{}
	backupRoot := filepath.Join(s.applyRoot, "backups", time.Now().UTC().Format("20060102T150405.000000000Z"))
	for _, stagedPath := range stagedPaths {
		livePath, ok := s.livePathForStagedConfig(stagedPath)
		if !ok {
			continue
		}
		body, err := os.ReadFile(stagedPath)
		if err != nil {
			return nil, nil, nil, err
		}
		record := livePromotionRecord{LivePath: livePath}
		if existing, err := os.ReadFile(livePath); err == nil {
			backupPath := filepath.Join(backupRoot, strings.TrimPrefix(filepath.ToSlash(livePath), "/"))
			if err := writeAtomicFile(backupPath, existing, 0o600); err != nil {
				return nil, nil, nil, err
			}
			record.HadPrevious = true
			record.BackupPath = backupPath
			backupFiles = append(backupFiles, backupPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil, err
		}
		if err := writeAtomicFile(livePath, body, 0o600); err != nil {
			return nil, nil, nil, err
		}
		liveFiles = append(liveFiles, livePath)
		records = append(records, record)
	}
	sort.Strings(liveFiles)
	sort.Strings(backupFiles)
	sort.Slice(records, func(i, j int) bool { return records[i].LivePath < records[j].LivePath })
	return liveFiles, backupFiles, records, nil
}

func (s *managementState) livePathForStagedConfig(stagedPath string) (string, bool) {
	slashPath := filepath.ToSlash(stagedPath)
	slashRoot := filepath.ToSlash(s.applyRoot)
	prefix := strings.TrimRight(slashRoot, "/") + "/generated/"
	if !strings.HasPrefix(slashPath, prefix) {
		return "", false
	}
	rel := strings.TrimPrefix(slashPath, prefix)
	switch rel {
	case "caddy/Caddyfile", "hysteria2/server.yaml", "sing-box/warp.json":
		return filepath.Join(s.applyRoot, "live", filepath.FromSlash(rel)), true
	default:
		return "", false
	}
}

func (s *managementState) reloadPromotedServicesLocked(liveFiles []string) []ServiceActionResult {
	commands := [][]string{}
	if containsPath(liveFiles, filepath.Join(s.applyRoot, "live", "caddy", "Caddyfile")) {
		commands = append(commands, []string{"systemctl", "reload", "veil-naive.service"})
	}
	if containsPath(liveFiles, filepath.Join(s.applyRoot, "live", "hysteria2", "server.yaml")) {
		commands = append(commands, []string{"systemctl", "reload", "veil-hysteria2.service"})
	}
	if containsPath(liveFiles, filepath.Join(s.applyRoot, "live", "sing-box", "warp.json")) {
		commands = append(commands, []string{"systemctl", "reload", "veil-warp.service"})
	}
	results := make([]ServiceActionResult, 0, len(commands))
	for _, command := range commands {
		result := serviceActionRunner(command)
		if result.Name == "" && len(command) > 0 {
			result.Name = command[len(command)-1]
		}
		if result.Command == nil {
			result.Command = append([]string(nil), command...)
		}
		results = append(results, result)
		if !result.Success {
			break
		}
	}
	return results
}

func (s *managementState) rollbackPromotedConfigsLocked(records []livePromotionRecord, liveFiles []string) ([]string, []ServiceActionResult) {
	rollbackFiles := []string{}
	for _, record := range records {
		if record.HadPrevious {
			body, err := os.ReadFile(record.BackupPath)
			if err != nil {
				continue
			}
			if err := writeAtomicFile(record.LivePath, body, 0o600); err != nil {
				continue
			}
			rollbackFiles = append(rollbackFiles, record.LivePath)
			continue
		}
		if err := os.Remove(record.LivePath); err == nil || errors.Is(err, os.ErrNotExist) {
			rollbackFiles = append(rollbackFiles, record.LivePath)
		}
	}
	sort.Strings(rollbackFiles)
	rollbackActions := []ServiceActionResult{}
	if len(rollbackFiles) > 0 {
		rollbackActions = s.reloadPromotedServicesLocked(liveFiles)
	}
	return rollbackFiles, rollbackActions
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

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
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

func runFixedServiceAction(command []string) ServiceActionResult {
	result := ServiceActionResult{Command: append([]string(nil), command...)}
	if len(command) > 0 {
		result.Name = command[len(command)-1]
	}
	if !isAllowedServiceCommand(command) {
		result.Error = "service command is not allowed"
		return result
	}
	binary, err := exec.LookPath(command[0])
	if err != nil {
		result.Error = command[0] + " not found"
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, command[1:]...)
	out, err := cmd.CombinedOutput()
	result.Output = strings.TrimSpace(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		result.Error = "service action timed out"
		return result
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Success = true
	return result
}

func runFixedServiceHealthCheck(service string) ServiceHealthResult {
	command := []string{"systemctl", "is-active", "--quiet", service}
	result := ServiceHealthResult{Name: service, Command: command}
	if !isAllowedHealthService(service) {
		result.Error = "service health check is not allowed"
		return result
	}
	binary, err := exec.LookPath(command[0])
	if err != nil {
		result.Error = command[0] + " not found"
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, command[1:]...)
	out, err := cmd.CombinedOutput()
	result.Output = strings.TrimSpace(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		result.Error = "service health check timed out"
		return result
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Healthy = true
	return result
}

func isAllowedServiceCommand(command []string) bool {
	if len(command) != 3 || command[0] != "systemctl" || command[1] != "reload" {
		return false
	}
	return isAllowedHealthService(command[2])
}

func isAllowedHealthService(service string) bool {
	return service == "veil-naive.service" || service == "veil-hysteria2.service" || service == "veil-warp.service"
}

func (s *managementState) renderManagementConfigsLocked() (map[string]string, error) {
	configs := map[string]string{}
	if s.hasRenderSettingsLocked() {
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
	}
	if s.warp.Enabled {
		body, err := s.renderWarpConfigLocked()
		if err != nil {
			return nil, err
		}
		configs[filepath.Join(s.applyRoot, "generated", "sing-box", "warp.json")] = body
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

func (s *managementState) renderWarpConfigLocked() (string, error) {
	warp := s.warp
	setWarpDefaults(&warp)
	return renderer.RenderWarpSingBox(renderer.WarpSingBoxConfig{
		Endpoint:      warp.Endpoint,
		PrivateKey:    warp.PrivateKey,
		LocalAddress:  warp.LocalAddress,
		PeerPublicKey: warp.PeerPublicKey,
		Reserved:      append([]int(nil), warp.Reserved...),
		SocksListen:   warp.SocksListen,
		SocksPort:     warp.SocksPort,
		MTU:           warp.MTU,
		RoutingRules:  renderWarpRoutingRules(s.rules),
	})
}

func renderWarpRoutingRules(rules []RoutingRule) []renderer.WarpRoutingRule {
	rendered := []renderer.WarpRoutingRule{}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		rendered = append(rendered, renderer.WarpRoutingRule{Match: rule.Match, Outbound: rule.Outbound})
	}
	return rendered
}

func runStagedConfigValidators(paths []string) []ConfigValidationResult {
	results := []ConfigValidationResult{}
	for _, path := range paths {
		slashPath := filepath.ToSlash(path)
		switch {
		case strings.HasSuffix(slashPath, "/generated/caddy/Caddyfile"):
			results = append(results, runFixedConfigValidation("caddy", path, []string{"caddy", "validate", "--config", path}))
		case strings.HasSuffix(slashPath, "/generated/hysteria2/server.yaml"):
			results = append(results, runFixedConfigValidation("hysteria2", path, []string{"hysteria", "server", "--config", path, "--check"}))
		case strings.HasSuffix(slashPath, "/generated/sing-box/warp.json"):
			results = append(results, runFixedConfigValidation("warp", path, []string{"sing-box", "check", "-c", path}))
		}
	}
	return results
}

func runFixedConfigValidation(name string, config string, command []string) ConfigValidationResult {
	result := ConfigValidationResult{Name: name, Config: config, Command: command}
	if len(command) == 0 {
		result.Skipped = true
		result.Error = "validator command is empty"
		return result
	}
	binary, err := exec.LookPath(command[0])
	if err != nil {
		result.Skipped = true
		result.Error = command[0] + " not found; syntax validation skipped"
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, command[1:]...)
	out, err := cmd.CombinedOutput()
	result.Output = strings.TrimSpace(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		result.Error = "validation timed out"
		return result
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Valid = true
	return result
}

func (s *managementState) snapshotLocked() managementSnapshot {
	return managementSnapshot{
		Settings:      s.settings,
		Inbounds:      append([]Inbound(nil), s.inbounds...),
		Rules:         append([]RoutingRule(nil), s.rules...),
		RoutingPreset: s.routingPreset,
		RoutingSource: s.routingSource,
		Warp:          s.warp,
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

func (s *managementState) inboundIndex(name string) int {
	for idx, inbound := range s.inbounds {
		if inbound.Name == name {
			return idx
		}
	}
	return -1
}

func (s *managementState) hasInboundTransportPort(transport string, port int) bool {
	return s.hasInboundTransportPortExcept(transport, port, -1)
}

func (s *managementState) hasInboundTransportPortExcept(transport string, port int, exceptIndex int) bool {
	for idx, existing := range s.inbounds {
		if idx == exceptIndex {
			continue
		}
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
	if snapshot.RoutingPreset != "" {
		s.routingPreset = snapshot.RoutingPreset
	}
	if snapshot.RoutingSource.Repository != "" || len(snapshot.RoutingSource.Files) > 0 {
		s.routingSource = snapshot.RoutingSource
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
