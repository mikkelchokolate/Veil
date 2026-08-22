package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"golang.org/x/crypto/bcrypt"
)

func TestLoginRecordsSuccessAndFailureAuditEvents(t *testing.T) {
	recorder := audit.NewRecorder(filepath.Join(t.TempDir(), "panel.jsonl"), audit.RecorderOptions{})
	passwordHash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	registry, _ := NewSessionRegistry("")
	now := time.Now()
	state := &managementState{
		audit:           recorder,
		sessions:        registry,
		loginBackoffNow: func() time.Time { return now },
		users: []User{{
			Username:     "alice",
			PasswordHash: string(passwordHash),
			Role:         "admin",
		}},
	}

	for index, password := range []string{"wrong-password", "correct-password"} {
		request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(
			`{"username":"alice","password":"`+password+`"}`,
		))
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "192.0.2.10:1234"
		response := httptest.NewRecorder()
		state.handleLogin(response, request)
		if index == 0 {
			now = now.Add(2 * time.Second)
		}
	}

	records, err := recorder.List(10, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("login audit records = %+v", records)
	}
	if records[0].Action != "auth.login" || !records[0].Success || records[0].Actor != "alice" {
		t.Fatalf("latest login record = %+v", records[0])
	}
	if records[1].Action != "auth.login" || records[1].Success {
		t.Fatalf("failed login record = %+v", records[1])
	}
}

func TestSetupCompletionRecordsAuditEvent(t *testing.T) {
	state := newTestSetupState(t, true)
	state.audit = audit.NewRecorder(filepath.Join(t.TempDir(), "panel.jsonl"), audit.RecorderOptions{})
	request := newSetupCompleteRequest()
	request.RemoteAddr = "127.0.0.1:9000"
	response := httptest.NewRecorder()

	state.handleSetupComplete(response, request)

	records, err := state.audit.List(10, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusCreated || len(records) != 1 ||
		records[0].Action != "setup.complete" || records[0].Actor != "admin" || !records[0].Success {
		t.Fatalf("status=%d records=%+v", response.Code, records)
	}
}

func TestAuditEndpointRequiresAdminAndReturnsBoundedHistory(t *testing.T) {
	recorder := audit.NewRecorder(filepath.Join(t.TempDir(), "panel.jsonl"), audit.RecorderOptions{})
	registry, _ := NewSessionRegistry("")
	state := &managementState{audit: recorder, sessions: registry}
	admin, _ := registry.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	viewer, _ := registry.Create(SessionCreateInput{Username: "bob", Role: "viewer"})
	for _, target := range []string{"one", "two", "three"} {
		if err := recorder.Append(audit.Record{Actor: "alice", Action: "test.event", Target: target, Success: true}); err != nil {
			t.Fatal(err)
		}
	}

	viewerRequest := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	viewerRequest.AddCookie(&http.Cookie{Name: "veil_session", Value: viewer.Token})
	viewerResponse := httptest.NewRecorder()
	state.handleAudit(viewerResponse, viewerRequest)
	if viewerResponse.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d body=%s", viewerResponse.Code, viewerResponse.Body.String())
	}

	adminRequest := httptest.NewRequest(http.MethodGet, "/api/audit?limit=2", nil)
	adminRequest.AddCookie(&http.Cookie{Name: "veil_session", Value: admin.Token})
	adminResponse := httptest.NewRecorder()
	state.handleAudit(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusOK {
		t.Fatalf("admin status=%d body=%s", adminResponse.Code, adminResponse.Body.String())
	}
	var payload AuditListResponse
	if err := json.NewDecoder(adminResponse.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 2 || payload.Items[0].Target != "three" || payload.NextBefore == "" {
		t.Fatalf("audit payload = %+v", payload)
	}
}
