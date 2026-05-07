package api

import (
	"context"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type LogRoutes struct{}

func (LogRoutes) Paths() []string {
	return []string{"/api/logs"}
}

func (routes LogRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/logs", routes.handleLogs)
}

func (LogRoutes) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	unit := r.URL.Query().Get("unit")
	if unit == "" {
		unit = "veil"
	}
	if !validLogUnit(unit) {
		writeError(w, "invalid unit name", http.StatusBadRequest)
		return
	}
	lines := 50
	if ls := r.URL.Query().Get("lines"); ls != "" {
		n, err := strconv.Atoi(ls)
		if err != nil || n < 1 || n > 500 {
			writeError(w, "lines must be 1-500", http.StatusBadRequest)
			return
		}
		lines = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx,
		"journalctl", "-u", unit+".service", "--no-pager", "-n", strconv.Itoa(lines), "-o", "short-iso",
	).CombinedOutput()
	if err != nil {
		writeError(w, "failed to read logs: "+strings.TrimSpace(string(out)), http.StatusServiceUnavailable)
		return
	}
	result := map[string]string{
		"unit":   unit,
		"output": string(out),
	}
	writeJSON(w, result)
}

// validLogUnit checks that a systemd unit name contains only safe characters.
func validLogUnit(unit string) bool {
	if unit == "" {
		return false
	}
	for _, r := range unit {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '@' || r == '.') {
			return false
		}
	}
	return true
}
