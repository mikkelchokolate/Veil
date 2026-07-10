package api

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
)

// bufferedResponseWriter lets the user route translate state-invariant
// conflicts without changing the response contract of unrelated failures.
type bufferedResponseWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newBufferedResponseWriter() *bufferedResponseWriter {
	return &bufferedResponseWriter{header: make(http.Header)}
}

func (w *bufferedResponseWriter) Header() http.Header {
	return w.header
}

func (w *bufferedResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *bufferedResponseWriter) flushTo(dst http.ResponseWriter) {
	for key, values := range w.header {
		for _, value := range values {
			dst.Header().Add(key, value)
		}
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	dst.WriteHeader(status)
	_, _ = dst.Write(w.body.Bytes())
}

func (s *managementState) handleUsersRouteWithAdminInvariant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		s.handleUsersRoute(w, r)
		return
	}

	captured := newBufferedResponseWriter()
	s.handleUsersRoute(captured, r)
	if captured.status == http.StatusInternalServerError && strings.Contains(captured.body.String(), managementstate.ErrLastAdministrator.Error()) {
		writeError(w, managementstate.ErrLastAdministrator.Error(), http.StatusBadRequest)
		return
	}
	captured.flushTo(w)
}
