package api

import (
	"encoding/json"
	"net/http"
)

type ServerInfo struct {
	Version string
	Mode    string
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

func NewRouter(info ServerInfo) http.Handler {
	mux := http.NewServeMux()
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
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
