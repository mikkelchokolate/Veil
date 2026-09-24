package api

import (
	"encoding/json"
	"errors"
	"fmt"
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

// findCaddyTLSCertPair locates a Caddy-managed certificate pair for the panel
// domain in Caddy's ACME storage. It is a package variable so tests can stub
// the filesystem search.
var findCaddyTLSCertPair = caddycert.FindPair

type RuntimeRoutes struct {
	// State supplies the panel settings (panelAccess/domain) the TLS endpoint
	// needs to locate and validate the served certificate. Nil keeps the
	// legacy VEIL_TLS_CERT-only behavior for embedded/test routers.
	State *managementState
}

func (r RuntimeRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/system", handleSystemRuntime)
	mux.HandleFunc("/api/tls", r.handleTLSRuntime)
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

// handleTLSRuntime reports the certificate the panel actually serves. Direct
// TLS mode points VEIL_TLS_CERT at a cert file; caddy panel access leaves the
// env unset because Caddy terminates TLS from its own ACME storage — in that
// case surface the Caddy-managed pair instead of a blind "no certificate
// path configured" (#905).
func (r RuntimeRoutes) handleTLSRuntime(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if req.Method == http.MethodGet {
		writeJSON(w, r.tlsCertInfo())
	}
}

// handleTLSRuntime preserves the legacy env-only behavior for callers that
// invoke the handler without a management state (tests, embedded routers).
func handleTLSRuntime(w http.ResponseWriter, req *http.Request) {
	(RuntimeRoutes{}).handleTLSRuntime(w, req)
}

func (r RuntimeRoutes) tlsCertInfo() veilruntime.TLSCertInfo {
	var settings Settings
	var serveAccess string
	if r.State != nil {
		r.State.mu.Lock()
		settings = r.State.settings
		serveAccess = r.State.servePanelAccess
		r.State.mu.Unlock()
	}
	envPath := strings.TrimSpace(os.Getenv("VEIL_TLS_CERT"))
	// A serve-time panelAccess override wins over the stored setting (#906).
	access := strings.TrimSpace(serveAccess)
	if access == "" {
		access = strings.TrimSpace(settings.PanelAccess)
	}
	expectedDomain := tlsExpectedDomain(settings)
	if envPath == "" && strings.EqualFold(access, "caddy") && expectedDomain != "" {
		pair, err := findCaddyTLSCertPair("", expectedDomain)
		if err == nil {
			info := veilruntime.ReadTLSCertForDomain(pair.CertPath, expectedDomain)
			info.Source = "caddy"
			// Surface which issuer actually served the managed certificate —
			// an ACME failure that silently fell back to Caddy's internal CA
			// is a degraded state, not a trusted issuance (#906).
			info.ManagedBy = "caddy"
			info.IssuerSource = pair.IssuerName
			info.IssuerKind = caddycert.IssuerKind(pair.IssuerName)
			return info
		}
		info := veilruntime.TLSCertInfo{Source: "caddy", ManagedBy: "caddy"}
		info.Error = fmt.Sprintf("no Caddy-managed certificate for %s: %v", expectedDomain, err)
		return info
	}
	info := veilruntime.ReadTLSCertForDomain(envPath, expectedDomain)
	if info.Path != "" {
		info.Source = "env"
	}
	return info
}

// tlsExpectedDomain returns the hostname the panel TLS certificate must
// cover: the panel domain under caddy access (PanelDomain falling back to
// Domain, matching caddyassembly.ResolveDomainCertSpecs), otherwise the
// primary domain. An empty result skips the SAN check.
func tlsExpectedDomain(settings Settings) string {
	if strings.EqualFold(strings.TrimSpace(settings.PanelAccess), "caddy") {
		if d := strings.Trim(strings.TrimSpace(settings.PanelDomain), "[]"); d != "" {
			return d
		}
	}
	return strings.Trim(strings.TrimSpace(settings.Domain), "[]")
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
