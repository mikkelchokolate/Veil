package api

import (
	"net/http"
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/service"
)

type LogRoutes struct{}

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
	result, err := service.NewLogReader(nil).Read(unit, lines)
	if err != nil {
		writeError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, result)
}

func validLogUnit(unit string) bool {
	return service.ValidLogUnit(unit)
}
