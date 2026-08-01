package api

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
)

type AuditListResponse struct {
	Items      []audit.Record `json:"items"`
	NextBefore string         `json:"nextBefore,omitempty"`
}

func (s *managementState) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 500 {
			writeError(w, "limit must be an integer between 1 and 500", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	var before time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("before")); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeError(w, "before must be an RFC3339 timestamp", http.StatusBadRequest)
			return
		}
		before = parsed
	}
	records, err := s.auditRecorder().List(limit, before)
	if err != nil {
		writeError(w, "failed to read audit history", http.StatusInternalServerError)
		return
	}
	response := AuditListResponse{Items: records}
	if len(records) == limit {
		response.NextBefore = records[len(records)-1].Timestamp.Format(time.RFC3339Nano)
	}
	writeJSON(w, response)
}

func (s *managementState) auditRecorder() *audit.Recorder {
	if s != nil && s.audit != nil {
		return s.audit
	}
	return audit.NewRecorder("", audit.RecorderOptions{})
}

func (s *managementState) recordRequestAudit(r *http.Request, record audit.Record) error {
	if r != nil {
		if record.Actor == "" {
			record.Actor, record.Role = s.auditActor(r)
		}
		record.IP = clientIP(r)
		record.UserAgent = r.UserAgent()
		record.RequestID = r.Header.Get("X-Request-ID")
		record.ClientRequestID = clientProvidedRequestID(r)
	}
	if record.Actor == "" {
		record.Actor = "system"
	}
	recorder := s.auditRecorder()
	if err := recorder.Append(record); err != nil {
		s.auditHealthMu.Lock()
		s.auditDegraded = true
		s.auditHealthMu.Unlock()
		log.Printf("SECURITY AUDIT DEGRADED: audit record persistence failed: %v", err)
		return err
	}
	if err := recorder.Degraded(); err != nil {
		s.auditHealthMu.Lock()
		s.auditDegraded = true
		s.auditHealthMu.Unlock()
		log.Printf("SECURITY AUDIT DEGRADED: primary audit unavailable; durable spool active: %v", err)
	} else {
		s.auditHealthMu.Lock()
		s.auditDegraded = false
		s.auditHealthMu.Unlock()
	}
	return nil
}

func (s *managementState) isAuditDegraded() bool {
	if s == nil {
		return false
	}
	s.auditHealthMu.RLock()
	defer s.auditHealthMu.RUnlock()
	return s.auditDegraded
}

func auditHealthMiddleware(state *managementState, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if state.isAuditDegraded() {
			w.Header().Set("X-Veil-Audit-Degraded", "true")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *managementState) auditActor(r *http.Request) (string, string) {
	username, _ := r.Context().Value(contextKeyUsername).(string)
	role, _ := r.Context().Value(contextKeyRole).(string)
	if username != "" {
		return username, role
	}
	if cookie, err := r.Cookie("veil_session"); err == nil {
		if session, ok := s.sessionRegistry().Get(cookie.Value); ok {
			return session.Username, session.Role
		}
	}
	return "api-token", "admin"
}
