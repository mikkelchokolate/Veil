package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/veil-panel/veil/internal/installer"
)

type ServerInfo struct {
	Version   string
	Mode      string
	AuthToken string
}

type StatusResponse struct {
	Name     string          `json:"name"`
	Version  string          `json:"version"`
	Mode     string          `json:"mode"`
	Services []ServiceStatus `json:"services"`
}

type ServiceStatus struct {
	Name      string `json:"name"`
	Managed   bool   `json:"managed"`
	Transport string `json:"transport,omitempty"`
}

type RURecommendedPreviewRequest struct {
	Domain string `json:"domain"`
	Email  string `json:"email"`
	Stack  string `json:"stack,omitempty"`
}

type RURecommendedPreviewResponse struct {
	Domain             string `json:"domain"`
	Email              string `json:"email"`
	Stack              string `json:"stack"`
	Port               int    `json:"port"`
	NaiveClientURL     string `json:"naiveClientURL"`
	Hysteria2ClientURI string `json:"hysteria2ClientURI"`
	Caddyfile          string `json:"caddyfile"`
	Hysteria2YAML      string `json:"hysteria2YAML"`
}

func NewRouter(info ServerInfo) http.Handler {
	mux := http.NewServeMux()
	newManagementState(info).register(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(panelHTML))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, StatusResponse{
			Name:    "Veil",
			Version: info.Version,
			Mode:    info.Mode,
			Services: []ServiceStatus{
				{Name: "veil", Managed: true},
				{Name: "naive", Managed: true, Transport: "tcp"},
				{Name: "hysteria2", Managed: true, Transport: "udp"},
			},
		})
	})
	mux.HandleFunc("/api/tools/speedtest", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		result, err := speedtestRunner(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, result)
	})
	mux.HandleFunc("/api/profiles/ru-recommended/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req RURecommendedPreviewRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		profile, err := installer.BuildRURecommendedProfile(installer.RURecommendedInput{
			Domain: req.Domain,
			Email:  req.Email,
			Stack:  installer.Stack(req.Stack),
			Availability: installer.PortAvailability{
				TCPBusy: map[int]bool{},
				UDPBusy: map[int]bool{},
			},
			Secret:     func(label string) string { return "preview-" + label },
			RandomPort: func() int { return 31874 },
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, RURecommendedPreviewResponse{
			Domain:             profile.Domain,
			Email:              profile.Email,
			Stack:              string(profile.Stack),
			Port:               profile.PortPlan.Port,
			NaiveClientURL:     redactProfileSecrets(profile, profile.NaiveClientURL),
			Hysteria2ClientURI: redactProfileSecrets(profile, profile.Hysteria2ClientURI),
			Caddyfile:          redactProfileSecrets(profile, profile.Caddyfile),
			Hysteria2YAML:      redactProfileSecrets(profile, profile.Hysteria2YAML),
		})
	})
	return authMiddleware(info.AuthToken, mux)
}

func redactProfileSecrets(profile installer.RURecommendedProfile, text string) string {
	for _, secret := range []string{profile.NaivePassword, profile.Hysteria2Password, profile.PanelAuthToken} {
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}

func authMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !validAuthToken(r, token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="Veil API"`)
			http.Error(w, "missing or invalid API token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validAuthToken(r *http.Request, want string) bool {
	provided := r.Header.Get("X-Veil-Token")
	if provided == "" {
		provided = bearerToken(r.Header.Get("Authorization"))
	}
	if provided == "" || len(provided) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(want)) == 1
}

func bearerToken(header string) string {
	if header == "" {
		return ""
	}
	prefix := "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
