package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/protocols"
	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
)

var runtimeTelemetryPolicy = protocols.ManagedProcessPolicy()

type RuntimeRoutes struct{}

func (RuntimeRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/system", handleSystemRuntime)
	mux.HandleFunc("/api/tls", handleTLSRuntime)
	mux.HandleFunc("/api/network", handleNetworkRuntime)
	mux.HandleFunc("/api/connections", handleConnectionsRuntime)
	mux.HandleFunc("/api/processes", handleProcessesRuntime)
	mux.HandleFunc("/api/disk", handleDiskRuntime)
	mux.HandleFunc("/api/runtime/observation", handleRuntimeObservation)
	mux.HandleFunc("/api/runtime/provenance", handleRuntimeProvenance)
}

func handleSystemRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		stats, err := veilruntime.NewRuntimeTelemetryWithPolicy(runtimeTelemetryPolicy).System()
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
		writeJSON(w, veilruntime.NewRuntimeTelemetryWithPolicy(runtimeTelemetryPolicy).TLS())
	}
}

func handleNetworkRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		stats, err := veilruntime.NewRuntimeTelemetryWithPolicy(runtimeTelemetryPolicy).Network()
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
		stats, err := veilruntime.NewRuntimeTelemetryWithPolicy(runtimeTelemetryPolicy).Connections()
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
		stats, err := veilruntime.NewRuntimeTelemetryWithPolicy(runtimeTelemetryPolicy).Processes()
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
		writeJSON(w, veilruntime.NewRuntimeTelemetryWithPolicy(runtimeTelemetryPolicy).Disk())
	}
}

func handleRuntimeProvenance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodHead {
		return
	}
	manifestPath := filepath.Join(runtimeinstall.DefaultBinDir(), ".veil-runtimes", "manifest.json")
	info, err := os.Lstat(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, map[string]any{"runtimes": map[string]any{}})
		return
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		writeError(w, "runtime provenance manifest is unavailable or invalid", http.StatusServiceUnavailable)
		return
	}
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		writeError(w, "runtime provenance manifest is unavailable", http.StatusServiceUnavailable)
		return
	}
	var manifest map[string]any
	if err := json.Unmarshal(body, &manifest); err != nil {
		writeError(w, "runtime provenance manifest is invalid", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, manifest)
}

func handleRuntimeObservation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		writeJSON(w, veilruntime.NewRuntimeTelemetryWithPolicy(runtimeTelemetryPolicy).Observation())
	}
}
