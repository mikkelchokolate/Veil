package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/service"
)

type LogRoutes struct {
	State *managementState
}

func (routes LogRoutes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/logs", routes.handleLogs)
}

func (routes LogRoutes) handleLogs(w http.ResponseWriter, r *http.Request) {
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
	runtime, ok := managedRuntimeByActionName(unit)
	if !ok {
		for _, candidate := range NewManagedRuntimeCatalog().Runtimes() {
			if candidate.Unit == unit || strings.TrimSuffix(candidate.Unit, ".service") == unit {
				runtime = candidate
				ok = true
				break
			}
		}
	}
	if !ok {
		writeError(w, "unknown managed unit", http.StatusBadRequest)
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
	if routes.State == nil || routes.State.privileged == nil {
		writePrivilegedError(w, &privileged.Error{
			Code: privileged.ErrorOperationFailed, Message: "privileged helper is unavailable",
		})
		return
	}
	journal, err := routes.State.privileged.Journal(r.Context(), privileged.JournalRequest{
		Unit: runtime.Unit, Lines: lines,
	})
	if err != nil {
		writePrivilegedError(w, err)
		return
	}
	writeJSON(w, service.LogResult{Unit: unit, Output: strings.Join(journal.Lines, "\n")})
}

func validLogUnit(unit string) bool {
	return service.ValidLogUnit(unit)
}
