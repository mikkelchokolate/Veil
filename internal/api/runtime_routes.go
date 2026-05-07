package api

import (
	"net/http"
	"os"
)

type RuntimeRoutes struct{}

func (RuntimeRoutes) Paths() []string {
	return []string{"/api/system", "/api/tls", "/api/network", "/api/connections", "/api/processes", "/api/disk"}
}

func (RuntimeRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/system", handleSystemRuntime)
	mux.HandleFunc("/api/tls", handleTLSRuntime)
	mux.HandleFunc("/api/network", handleNetworkRuntime)
	mux.HandleFunc("/api/connections", handleConnectionsRuntime)
	mux.HandleFunc("/api/processes", handleProcessesRuntime)
	mux.HandleFunc("/api/disk", handleDiskRuntime)
}

func handleSystemRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		stats, err := readSystemStats()
		if err != nil {
			writeError(w, "failed to read system stats", http.StatusInternalServerError)
			return
		}
		writeJSON(w, stats)
	}
}

func handleTLSRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		certPath := os.Getenv("VEIL_TLS_CERT")
		writeJSON(w, readTLSCert(certPath))
	}
}

func handleNetworkRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		stats, err := readNetworkStats()
		if err != nil {
			writeError(w, "failed to read network stats", http.StatusInternalServerError)
			return
		}
		writeJSON(w, stats)
	}
}

func handleConnectionsRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		stats, err := readConnectionsStats()
		if err != nil {
			writeError(w, "failed to read connections", http.StatusInternalServerError)
			return
		}
		writeJSON(w, stats)
	}
}

func handleProcessesRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		stats, err := readProcessesStats()
		if err != nil {
			writeError(w, "failed to read processes", http.StatusInternalServerError)
			return
		}
		writeJSON(w, stats)
	}
}

func handleDiskRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		writeJSON(w, readDirDiskStats())
	}
}
