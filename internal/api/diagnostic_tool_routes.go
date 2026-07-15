package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/diagnostics"
)

type PingResult = diagnostics.PingResult
type SpeedtestResult = diagnostics.SpeedtestResult

var dnsLookuper = diagnostics.RunDNSLookup
var pingRunner = diagnostics.RunPing
var speedtestRunner = diagnostics.RunSpeedtest
var errSpeedtestUnavailable = diagnostics.ErrSpeedtestUnavailable

type DiagnosticToolRoutes struct{}

func (routes DiagnosticToolRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/tools/dns-lookup", routes.handleDNSLookup)
	mux.HandleFunc("/api/tools/ping", routes.handlePing)
	mux.HandleFunc("/api/tools/speedtest", routes.handleSpeedtest)
}

func validateDiagnosticTarget(target string) error {
	if len(target) > 255 {
		return errors.New("target must be at most 255 characters")
	}
	if strings.HasPrefix(target, "-") {
		return errors.New("target must not begin with '-'")
	}
	if strings.ContainsAny(target, " \t\r\n\x00") {
		return errors.New("target must not contain whitespace or NUL characters")
	}
	return nil
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
	req.Hostname = strings.TrimSpace(req.Hostname)
	if req.Hostname == "" {
		writeError(w, "hostname is required", http.StatusBadRequest)
		return
	}
	if err := validateDiagnosticTarget(req.Hostname); err != nil {
		writeError(w, "hostname: "+err.Error(), http.StatusBadRequest)
		return
	}
	addrs, cname, err := dnsLookuper(req.Hostname)
	writeJSON(w, diagnostics.NewDNSLookupResult(req.Hostname, addrs, cname, err).Map())
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
	req.Host = strings.TrimSpace(req.Host)
	if req.Host == "" {
		writeError(w, "host is required", http.StatusBadRequest)
		return
	}
	if err := validateDiagnosticTarget(req.Host); err != nil {
		writeError(w, "host: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Count == 0 {
		req.Count = 3
	}
	if req.Count < 1 || req.Count > 10 {
		writeError(w, "count must be 1-10", http.StatusBadRequest)
		return
	}
	writeJSON(w, pingRunner(req.Host, req.Count))
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
