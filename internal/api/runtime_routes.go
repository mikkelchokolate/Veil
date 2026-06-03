package api

import (
	"net/http"

	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
)

type RuntimeRoutes struct{}

func (RuntimeRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/system", handleSystemRuntime)
	mux.HandleFunc("/api/tls", handleTLSRuntime)
	mux.HandleFunc("/api/network", handleNetworkRuntime)
	mux.HandleFunc("/api/connections", handleConnectionsRuntime)
	mux.HandleFunc("/api/processes", handleProcessesRuntime)
	mux.HandleFunc("/api/disk", handleDiskRuntime)
	mux.HandleFunc("/api/runtime/observation", handleRuntimeObservation)
}

func handleSystemRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		stats, err := veilruntime.NewRuntimeTelemetry().System()
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
		writeJSON(w, veilruntime.NewRuntimeTelemetry().TLS())
	}
}

func handleNetworkRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		stats, err := veilruntime.NewRuntimeTelemetry().Network()
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
		stats, err := veilruntime.NewRuntimeTelemetry().Connections()
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
		stats, err := veilruntime.NewRuntimeTelemetry().Processes()
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
		writeJSON(w, veilruntime.NewRuntimeTelemetry().Disk())
	}
}

func handleRuntimeObservation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		writeJSON(w, veilruntime.NewRuntimeTelemetry().Observation())
	}
}
