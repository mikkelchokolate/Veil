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
	output := sanitizeServiceLogOutput(strings.Join(journal.Lines, "\n"))
	writeJSON(w, service.LogResult{Unit: resolvedUnit, Output: output})
}

func (routes LogRoutes) resolveLogUnit(unit string) (string, bool) {
	var visibleRuntimes []ManagedRuntime
	if routes.State != nil {
		visibleRuntimes = NewVisibleManagedRuntimeCatalogForState(routes.State).Runtimes()
		if resolved, ok := matchLogUnit(visibleRuntimes, unit); ok {
			return resolved, true
		}
	}
	for _, candidate := range NewManagedRuntimeCatalog().Runtimes() {
		if !logUnitCandidateMatch(candidate, unit) {
			continue
		}
		// A bare template unit (veil-hysteria2@.service) never holds logs once
		// live per-inbound instances exist. journalctl -u accepts unit
		// patterns, so resolve to every live instance of the template instead
		// of journaling the empty template unit.
		if candidate.TemplateUnit != "" && candidate.Unit == candidate.TemplateUnit &&
			hasVisibleTemplateInstance(visibleRuntimes, candidate.TemplateUnit) {
			return templateInstanceLogPattern(candidate.TemplateUnit), true
		}
		return candidate.Unit, true
	}
	return "", false
}

func matchLogUnit(runtimes []ManagedRuntime, unit string) (string, bool) {
	for _, candidate := range runtimes {
		if logUnitCandidateMatch(candidate, unit) {
			return candidate.Unit, true
		}
	}
	return "", false
}

func logUnitCandidateMatch(candidate ManagedRuntime, unit string) bool {
	if candidate.ActionName == unit || candidate.Name == unit {
		return true
	}
	return candidate.Unit == unit || strings.TrimSuffix(candidate.Unit, ".service") == unit
}

func hasVisibleTemplateInstance(runtimes []ManagedRuntime, templateUnit string) bool {
	for _, candidate := range runtimes {
		if candidate.TemplateUnit == templateUnit && candidate.Unit != templateUnit {
			return true
		}
	}
	return false
}

// templateInstanceLogPattern maps veil-hysteria2@.service to the journalctl
// unit pattern veil-hysteria2@*.service, which selects all live instances.
func templateInstanceLogPattern(templateUnit string) string {
	return strings.TrimSuffix(templateUnit, ".service") + "*.service"
}

func validLogUnit(unit string) bool {
	return service.ValidLogUnit(unit)
}
