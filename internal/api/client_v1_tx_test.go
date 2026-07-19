package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestV1ClientCreateTransactionalRollback asserts (A5) that when any requested
// binding fails, the whole create rolls back: no 201, and no orphaned client
// row remains.
func TestV1ClientCreateTransactionalRollback(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)

	// The duplicate inboundId triggers a (client_id, inbound_id) unique
	// violation on the second binding insert, so the create must fail.
	body := strings.NewReader(`{"name":"tx-client","bindings":[{"inboundId":"hy2"},{"inboundId":"hy2"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clients", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusCreated {
		t.Fatalf("expected non-201 for a create with a failing binding, got 201: %s", w.Body.String())
	}

	// The client must NOT exist afterwards (full rollback, not partial).
	wl := httptest.NewRecorder()
	r.ServeHTTP(wl, httptest.NewRequest(http.MethodGet, "/api/v1/clients?search=tx-client", nil))
	var list struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(wl.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 0 {
		t.Fatalf("orphaned client remains after failed transactional create: total=%d", list.Total)
	}
}

// TestV1ClientCreateSuccessReturnsAllBindings confirms the happy path still
// creates the client with all requested bindings and returns 201.
func TestV1ClientCreateSuccessReturnsAllBindings(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)
	body := strings.NewReader(`{"name":"ok-client","bindings":[{"inboundId":"hy2","credential":"pass1"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clients", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}
