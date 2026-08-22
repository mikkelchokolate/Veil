package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const expectedMaxServiceLogResponseBytes = 256 * 1024

func TestRequestIDMiddlewareReplacesClientIDAndErrorUsesServerID(t *testing.T) {
	var serverID, clientID string
	handler := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID = r.Header.Get("X-Request-ID")
		clientID = clientProvidedRequestID(r)
		writeError(w, "bad request", http.StatusBadRequest)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("X-Request-ID", "client-controlled")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if serverID == "" || serverID == "client-controlled" || clientID != "client-controlled" || rec.Header().Get("X-Request-ID") != serverID {
		t.Fatalf("server=%q client=%q response=%q", serverID, clientID, rec.Header().Get("X-Request-ID"))
	}
	if !strings.Contains(rec.Body.String(), `"requestId":"`+serverID+`"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestInternalErrorEnvelopeIsStandardAndDoesNotLeakPaths(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("X-Request-ID", "server-request-id")
	writeError(rec, "open /var/lib/veil/private/state.key: permission denied", http.StatusInternalServerError)
	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"requestId"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "internal_error" || body.Error.Message != "internal server error" || body.Error.RequestID != "server-request-id" {
		t.Fatalf("unexpected envelope: %+v body=%s", body, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "/var/lib/veil") {
		t.Fatal("filesystem path leaked")
	}
}

func TestLogsAreRedactedAndByteBounded(t *testing.T) {
	secret := "top-secret-token"
	client := &recordingPrivilegedClient{journalLines: []string{
		"Authorization: Bearer " + secret,
		"password=" + secret,
		`{"privateKey":"secret value with spaces"}`,
		"-----BEGIN PRIVATE KEY-----\nPEMSECRET\n-----END PRIVATE KEY-----",
		strings.Repeat("x", 400000),
	}}
	state := &managementState{privileged: client}
	req := httptest.NewRequest(http.MethodGet, "/api/logs?unit=veil&lines=500", nil)
	rec := httptest.NewRecorder()
	(LogRoutes{State: state}).handleLogs(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secret) || strings.Contains(rec.Body.String(), "secret value with spaces") || strings.Contains(rec.Body.String(), "PEMSECRET") || !strings.Contains(rec.Body.String(), "[REDACTED]") {
		t.Fatalf("secret was not redacted: %.300s", rec.Body.String())
	}
	if rec.Body.Len() > expectedMaxServiceLogResponseBytes+1024 {
		t.Fatalf("response bytes=%d, limit=%d", rec.Body.Len(), expectedMaxServiceLogResponseBytes)
	}
}
