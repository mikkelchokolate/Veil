package api

import (
	"bytes"
	"encoding/json"
	"io"
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

type userMutationPreview struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func validateUserMutationBody(w http.ResponseWriter, r *http.Request, requireUsername bool) bool {
	if contentType := r.Header.Get("Content-Type"); contentType != "" && !isJSONMediaType(contentType) {
		// Preserve canonical 415 handling in decodeJSONRequest.
		return true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBodyBytes+1))
	if err != nil {
		writeError(w, "failed to read request body", http.StatusBadRequest)
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if int64(len(body)) > maxJSONBodyBytes {
		// Leave canonical oversized-body handling to decodeJSONRequest.
		return true
	}
	var request userMutationPreview
	if err := json.Unmarshal(body, &request); err != nil {
		// Preserve the canonical invalid JSON response.
		return true
	}
	if requireUsername && request.Username != "" && !validSetupUsername(request.Username) {
		writeError(w, "username must be 3-64 characters using letters, digits, dot, underscore, or hyphen", http.StatusBadRequest)
		return false
	}
	if request.Password != "" && len(request.Password) < 12 {
		writeError(w, "password must be at least 12 characters", http.StatusBadRequest)
		return false
	}
	return true
}

func exactUserNamePath(path string) bool {
	name := strings.TrimPrefix(path, "/api/users/")
	return name != "" && name != path && !strings.Contains(name, "/")
}

func (s *managementState) handleUsersRouteWithAdminInvariant(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/users" {
		switch r.Method {
		case http.MethodGet:
			s.handleUsersRoute(w, r)
			return
		case http.MethodPost:
			if validateUserMutationBody(w, r, true) {
				s.handleUsersRoute(w, r)
			}
			return
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
			return
		}
	}

	if !exactUserNamePath(r.URL.Path) {
		writeNotFound(w)
		return
	}
	if r.Method != http.MethodPut && r.Method != http.MethodDelete {
		methodNotAllowed(w, http.MethodPut, http.MethodDelete)
		return
	}
	if r.Method == http.MethodPut && !validateUserMutationBody(w, r, false) {
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
