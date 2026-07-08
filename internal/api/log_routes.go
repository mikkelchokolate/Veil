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
	resolvedUnit, ok := routes.resolveLogUnit(unit)
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
		Unit: resolvedUnit, Lines: lines,
	})
	if err != nil {
		writePrivilegedError(w, err)
		return
	}
	writeJSON(w, service.LogResult{Unit: resolvedUnit, Output: strings.Join(journal.Lines, "\n")})
}

func (routes LogRoutes) resolveLogUnit(unit string) (string, bool) {
	for _, catalog := range routes.logUnitCatalogs() {
		for _, candidate := range catalog.Runtimes() {
			if candidate.ActionName == unit || candidate.Name == unit {
				return candidate.Unit, true
			}
			if candidate.Unit == unit || strings.TrimSuffix(candidate.Unit, ".service") == unit {
				return candidate.Unit, true
			}
		}
		policy := service.NewCommandPolicy(catalog)
		if strings.HasSuffix(unit, ".service") && policy.AllowsHealth(unit) {
			return unit, true
		}
	}
	return "", false
}

func (routes LogRoutes) logUnitCatalogs() []ManagedRuntimeCatalog {
	catalogs := []ManagedRuntimeCatalog{}
	if routes.State != nil {
		catalogs = append(catalogs, NewManagedRuntimeCatalogFor(routes.State.inbounds, routes.State.warp))
	}
	catalogs = append(catalogs, NewManagedRuntimeCatalog())
	return catalogs
}

func validLogUnit(unit string) bool {
	return service.ValidLogUnit(unit)
}
