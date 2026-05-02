package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"strings"

	"github.com/veil-panel/veil/internal/installer"
)

type ServerInfo struct {
	Version   string
	Mode      string
	AuthToken string
	StatePath string
	ApplyRoot string
}

type StatusResponse struct {
	Name     string          `json:"name"`
	Version  string          `json:"version"`
	Mode     string          `json:"mode"`
	Services []ServiceStatus `json:"services"`
}

type ServiceStatus struct {
	Name        string `json:"name"`
	Managed     bool   `json:"managed"`
	Transport   string `json:"transport,omitempty"`
	Unit        string `json:"unit,omitempty"`
	LoadState   string `json:"loadState,omitempty"`
	ActiveState string `json:"activeState,omitempty"`
	SubState    string `json:"subState,omitempty"`
	Error       string `json:"error,omitempty"`
}

type ServiceRuntimeStatus struct {
	Unit        string
	LoadState   string
	ActiveState string
	SubState    string
	Error       string
}

var serviceStatusReader = readSystemdServiceStatus

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
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
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
			Name:     "Veil",
			Version:  info.Version,
			Mode:     info.Mode,
			Services: buildServiceStatuses(),
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
		if !decodeJSONRequest(w, r, &req) {
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

func buildServiceStatuses() []ServiceStatus {
	services := []ServiceStatus{
		{Name: "veil", Managed: true, Unit: "veil.service"},
		{Name: "naive", Managed: true, Transport: "tcp", Unit: "caddy.service"},
		{Name: "hysteria2", Managed: true, Transport: "udp", Unit: "hysteria2.service"},
		{Name: "sing-box", Managed: true, Unit: "sing-box.service"},
	}
	for i := range services {
		runtime := serviceStatusReader(services[i].Unit)
		services[i].LoadState = runtime.LoadState
		services[i].ActiveState = runtime.ActiveState
		services[i].SubState = runtime.SubState
		services[i].Error = runtime.Error
	}
	return services
}

func readSystemdServiceStatus(unit string) ServiceRuntimeStatus {
	status := ServiceRuntimeStatus{Unit: unit, LoadState: "unknown", ActiveState: "unknown", SubState: "unknown"}
	output, err := exec.Command(
		"systemctl",
		"show",
		unit,
		"--property=LoadState",
		"--property=ActiveState",
		"--property=SubState",
		"--no-page",
	).CombinedOutput()
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			if value != "" {
				status.LoadState = value
			}
		case "ActiveState":
			if value != "" {
				status.ActiveState = value
			}
		case "SubState":
			if value != "" {
				status.SubState = value
			}
		}
	}
	if err != nil {
		status.Error = strings.TrimSpace(string(output))
		if status.Error == "" {
			status.Error = err.Error()
		}
	}
	return status
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

const maxJSONBodyBytes int64 = 1024 * 1024

func decodeJSONRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)).Decode(v)
	if err == nil {
		return true
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return false
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
	return false
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = json.NewEncoder(w).Encode(v)
}
