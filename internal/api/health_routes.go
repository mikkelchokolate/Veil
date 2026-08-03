package api

import (
	"context"
	"net/http"
	"time"
)

type healthComponent struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type healthResponse struct {
	Status     string                     `json:"status"`
	Components map[string]healthComponent `json:"components"`
}

type HealthRoutes struct {
	State *managementState
}

func (routes HealthRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/livez", routes.handleLive)
	mux.HandleFunc("/readyz", routes.handleReady)
	mux.HandleFunc("/api/health", routes.handleAPIHealth)
}

func (routes HealthRoutes) handleLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		writeJSON(w, map[string]string{"status": "alive"})
	}
}

func (routes HealthRoutes) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	response, ready := routes.snapshot(r.Context())
	setJSONHeaders(w)
	if !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if r.Method == http.MethodGet {
		writeJSON(w, response)
	}
}

func (routes HealthRoutes) handleAPIHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	response, _ := routes.snapshot(r.Context())
	setJSONHeaders(w)
	if r.Method == http.MethodGet {
		writeJSON(w, response)
	}
}

func (routes HealthRoutes) snapshot(parent context.Context) (healthResponse, bool) {
	components := map[string]healthComponent{
		"state_store":          {Status: "ok"},
		"sqlite":               {Status: "ok"},
		"apply_lease":          {Status: "ok"},
		"publication_recovery": {Status: "ok"},
		"privileged_helper":    {Status: "ok"},
		"runtime_services":     {Status: "unknown"},
		"firewall":             {Status: "unknown"},
		"audit_primary":        {Status: "ok"},
		"audit_spool":          {Status: "ok"},
		"session_store":        {Status: "ok"},
		"traffic_providers":    {Status: "unknown"},
		"quota_enforcement":    {Status: "unknown"},
		"expiry_enforcement":   {Status: "unknown"},
		"backup_restore":       {Status: "ok"},
		"update_job":           {Status: "unknown"},
	}
	ready := true
	state := routes.State
	if state == nil {
		components["state_store"] = healthComponent{Status: "degraded", Reason: "unavailable"}
		components["sqlite"] = healthComponent{Status: "degraded", Reason: "unavailable"}
		return healthResponse{Status: "degraded", Components: components}, false
	}

	state.mu.Lock()
	stateLoadFailed := state.startupStateLoadFailed
	storageErr := state.storageDegradedErr
	restoreRunning := state.clientSubsystemStopping
	runtimeUnknown := state.runtimeVerificationUnknown
	sessionRegistry := state.sessions
	sessionsAvailable := sessionRegistry != nil
	requireHelper := state.requirePrivilegedHelper
	helperAvailable := state.privileged != nil
	state.mu.Unlock()
	if stateLoadFailed {
		components["state_store"] = healthComponent{Status: "degraded", Reason: "load_or_integrity_failed"}
		ready = false
	}
	if storageErr != nil || state.db == nil {
		components["sqlite"] = healthComponent{Status: "degraded", Reason: "storage_unavailable"}
		ready = false
	} else {
		ctx, cancel := context.WithTimeout(parent, 250*time.Millisecond)
		if err := state.db.PingContext(ctx); err != nil {
			components["sqlite"] = healthComponent{Status: "degraded", Reason: "storage_unavailable"}
			ready = false
		}
		cancel()
		var active int
		if err := state.db.QueryRow(`SELECT COUNT(*) FROM runtime_publications`).Scan(&active); err != nil {
			components["publication_recovery"] = healthComponent{Status: "degraded", Reason: "state_unavailable"}
			ready = false
		} else if active > 0 {
			components["publication_recovery"] = healthComponent{Status: "recovering", Reason: "unresolved_publication"}
			ready = false
		}
	}
	if state.applyRunner != nil {
		if err := state.applyRunner.ReadinessError(); err != nil {
			components["publication_recovery"] = healthComponent{Status: "degraded", Reason: "recovery_failed"}
			ready = false
		}
	} else if state.requireApplyTracking {
		components["apply_lease"] = healthComponent{Status: "degraded", Reason: "runner_unavailable"}
		ready = false
	}
	if requireHelper && !helperAvailable {
		components["privileged_helper"] = healthComponent{Status: "degraded", Reason: "unavailable"}
		ready = false
	}
	if !sessionsAvailable {
		components["session_store"] = healthComponent{Status: "degraded", Reason: "unavailable"}
		ready = false
	} else if err := sessionRegistry.Healthy(); err != nil {
		components["session_store"] = healthComponent{Status: "degraded", Reason: "persistence_failure"}
		ready = false
	}
	if state.isAuditDegraded() {
		components["audit_primary"] = healthComponent{Status: "degraded", Reason: "primary_unavailable"}
		components["audit_spool"] = healthComponent{Status: "degraded", Reason: "durability_unverified"}
		ready = false
	}
	if runtimeUnknown {
		components["runtime_services"] = healthComponent{Status: "recovering", Reason: "runtime_unknown"}
		ready = false
	}
	if restoreRunning {
		components["backup_restore"] = healthComponent{Status: "recovering", Reason: "restore_in_progress"}
		ready = false
	}
	status := "ok"
	if !ready {
		status = "degraded"
	}
	return healthResponse{Status: status, Components: components}, ready
}
