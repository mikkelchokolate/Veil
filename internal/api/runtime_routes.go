package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/caddycert"
	"github.com/mikkelchokolate/Veil/internal/protocols"
	veilruntime "github.com/mikkelchokolate/Veil/internal/runtime"
	"github.com/mikkelchokolate/Veil/internal/runtimeinstall"
)

var runtimeTelemetryPolicy = protocols.ManagedProcessPolicy()

type RuntimeRoutes struct {
	// TLSInfo reports the certificate status shown by /api/tls. When nil the
	// handler only reads the VEIL_TLS_CERT file. RouterComposition wires the
	// management-state variant so the Caddy-managed panel cert (and which
	// issuer actually served it) is visible too (#906).
	TLSInfo func() veilruntime.TLSCertInfo
}

// defaultTLSInfo is the environment-only certificate reader used when no
// management state is wired into RuntimeRoutes.
func defaultTLSInfo() veilruntime.TLSCertInfo {
	return veilruntime.NewRuntimeTelemetryWithPolicy(runtimeTelemetryPolicy).TLS()
}

// caddyPanelCertPair is a seam so tests can exercise the Caddy-certificate
// fallback without touching /var/lib/caddy.
var caddyPanelCertPair = caddycert.FindPair

func (r RuntimeRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/system", handleSystemRuntime)
	tlsInfo := r.TLSInfo
	if tlsInfo == nil {
		tlsInfo = defaultTLSInfo
	}
	mux.HandleFunc("/api/tls", func(w http.ResponseWriter, req *http.Request) {
		handleTLSRuntime(w, req, tlsInfo)
	})
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

func handleTLSRuntime(w http.ResponseWriter, r *http.Request, tlsInfo func() veilruntime.TLSCertInfo) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		if tlsInfo == nil {
			tlsInfo = defaultTLSInfo
		}
		writeJSON(w, tlsInfo())
	}
}

// tlsCertInfo reports the certificate status for /api/tls. When the panel runs
// behind the managed Caddy site (panelAccess=caddy) and no explicit
// VEIL_TLS_CERT is configured, the public certificate lives in Caddy's issuer
// storage — report it with its issuer source so an ACME failure that silently
// fell back to Caddy's internal CA is visible instead of showing "no path"
// (#906).
func (s *managementState) tlsCertInfo() veilruntime.TLSCertInfo {
	info := defaultTLSInfo()
	if info.Path != "" {
		return info
	}
	s.mu.Lock()
	settings := s.settings
	serveAccess := s.servePanelAccess
	s.mu.Unlock()
	access := strings.TrimSpace(serveAccess)
	if access == "" {
		access = strings.TrimSpace(settings.PanelAccess)
	}
	if access != "caddy" {
		return info
	}
	domain := strings.TrimSpace(settings.PanelDomain)
	if domain == "" {
		domain = strings.TrimSpace(settings.Domain)
	}
	if domain == "" {
		info.Error = "caddy panel certificate: no panel domain configured"
		return info
	}
	pair, err := caddyPanelCertPair("", domain)
	if err != nil {
		info.Error = "caddy panel certificate: " + err.Error()
		return info
	}
	info = veilruntime.ReadTLSCert(pair.CertPath)
	info.ManagedBy = "caddy"
	info.IssuerSource = pair.IssuerName
	info.IssuerKind = caddycert.IssuerKind(pair.IssuerName)
	return info
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
