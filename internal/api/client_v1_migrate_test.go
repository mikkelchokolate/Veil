package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestV1MigrateLegacyConvertsProfiles asserts (A6b) that the migrate-legacy
// endpoint converts embedded inbound profiles into normalized clients, is
// idempotent on re-run, and that the migrated client is retrievable via the v1
// client API.
func TestV1MigrateLegacyConvertsProfiles(t *testing.T) {
	r, _ := newApplyTrackedRouter(t)

	// Create an inbound with an embedded legacy profile.
	inboundBody := strings.NewReader(`{"name":"hy2","protocol":"hysteria2","transport":"udp","port":18443,"enabled":true,"profiles":[{"name":"legacybob","username":"legacybob","password":"bob-pass","enabled":true}]}`)
	iw := httptest.NewRecorder()
	ireq := httptest.NewRequest(http.MethodPost, "/api/inbounds", inboundBody)
	ireq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(iw, ireq)
	if iw.Code != http.StatusOK && iw.Code != http.StatusCreated {
		t.Fatalf("create inbound: %d %s", iw.Code, iw.Body.String())
	}

	// Run migration.
	mw := httptest.NewRecorder()
	r.ServeHTTP(mw, httptest.NewRequest(http.MethodPost, "/api/v1/clients/migrate-legacy", strings.NewReader(`{}`)))
	if mw.Code != http.StatusOK {
		t.Fatalf("migrate: %d %s", mw.Code, mw.Body.String())
	}
	var mresp map[string]any
	if err := json.NewDecoder(mw.Body).Decode(&mresp); err != nil {
		t.Fatalf("decode migrate: %v", err)
	}
	created, _ := mresp["clientsCreated"].(float64)
	if created < 1 {
		t.Fatalf("expected >=1 migrated client, got %v: %v", created, mresp)
	}

	// The migrated client must be retrievable via the v1 client API.
	lw := httptest.NewRecorder()
	r.ServeHTTP(lw, httptest.NewRequest(http.MethodGet, "/api/v1/clients?search=legacybob", nil))
	var lresp struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(lw.Body).Decode(&lresp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if lresp.Total < 1 {
		t.Fatalf("migrated client not found via v1 clients API: total=%d", lresp.Total)
	}

	// Idempotent: a second run creates nothing new.
	mw2 := httptest.NewRecorder()
	r.ServeHTTP(mw2, httptest.NewRequest(http.MethodPost, "/api/v1/clients/migrate-legacy", strings.NewReader(`{}`)))
	var mresp2 map[string]any
	if err := json.NewDecoder(mw2.Body).Decode(&mresp2); err != nil {
		t.Fatalf("decode migrate2: %v", err)
	}
	created2, _ := mresp2["clientsCreated"].(float64)
	if created2 != 0 {
		t.Fatalf("migration not idempotent: second run created %v clients", created2)
	}
}
