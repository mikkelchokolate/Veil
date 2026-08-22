package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
)

func TestAuditPersistenceFailureSetsVisibleDegradedState(t *testing.T) {
	state := &managementState{audit: audit.NewRecorder(t.TempDir(), audit.RecorderOptions{})}
	if err := state.recordRequestAudit(nil, audit.Record{Action: "security.test", Success: true}); err == nil {
		t.Fatal("expected audit persistence failure")
	}
	rec := httptest.NewRecorder()
	auditHealthMiddleware(state, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("X-Veil-Audit-Degraded") != "true" {
		t.Fatalf("header=%q", rec.Header().Get("X-Veil-Audit-Degraded"))
	}
}

func TestAuditSeparatesServerAndClientRequestIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	state := &managementState{audit: audit.NewRecorder(path, audit.RecorderOptions{})}
	handler := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := state.recordRequestAudit(r, audit.Record{Action: "request.test", Success: true}); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "client-id")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	records, err := state.audit.List(1, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RequestID == "" || records[0].RequestID == "client-id" || records[0].ClientRequestID != "client-id" {
		t.Fatalf("records=%+v", records)
	}
}
