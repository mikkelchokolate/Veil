package api

import (
	"net/http"
	"strings"
)

type DiagnosticToolRoutes struct{}

func (DiagnosticToolRoutes) Paths() []string {
	return []string{"/api/tools/dns-lookup", "/api/tools/ping", "/api/tools/speedtest"}
}

func (routes DiagnosticToolRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/tools/dns-lookup", routes.handleDNSLookup)
	mux.HandleFunc("/api/tools/ping", routes.handlePing)
	mux.HandleFunc("/api/tools/speedtest", routes.handleSpeedtest)
}

func (DiagnosticToolRoutes) handleDNSLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var req struct {
		Hostname string `json:"hostname"`
	}
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Hostname) == "" {
		writeError(w, "hostname is required", http.StatusBadRequest)
		return
	}
	addrs, cname, err := dnsLookuper(req.Hostname)
	result := map[string]any{
		"hostname":  req.Hostname,
		"addresses": addrs,
	}
	if cname != "" {
		result["cname"] = cname
	}
	if err != nil {
		result["error"] = err.Error()
	}
	if addrs == nil {
		result["addresses"] = []string{}
	}
	writeJSON(w, result)
}

func (DiagnosticToolRoutes) handlePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var req struct {
		Host  string `json:"host"`
		Count int    `json:"count"`
	}
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Host) == "" {
		writeError(w, "host is required", http.StatusBadRequest)
		return
	}
	if req.Count <= 0 {
		req.Count = 3
	}
	if req.Count > 10 {
		writeError(w, "count must be 1-10", http.StatusBadRequest)
		return
	}
	result := pingRunner(req.Host, req.Count)
	writeJSON(w, result)
}

func (DiagnosticToolRoutes) handleSpeedtest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if err := validateEmptyJSONBody(r); err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := speedtestRunner(r)
	if err != nil {
		writeError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, result)
}
