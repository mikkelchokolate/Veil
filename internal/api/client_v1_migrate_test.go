package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
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

// TestV1MigrateLegacyCommitsRepairOnlyChanges covers #983: when an earlier
// migration left a committed client whose credential never made it, a repair
// re-run must COMMIT the recreated credential instead of rolling it back as
// "no client changes" while still reporting success.
func TestV1MigrateLegacyCommitsRepairOnlyChanges(t *testing.T) {
	r, st := newApplyTrackedRouterWithState(t)

	inboundBody := strings.NewReader(`{"name":"hy2","protocol":"hysteria2","transport":"udp","port":18443,"enabled":true,"profiles":[{"name":"legacybob","username":"legacybob","password":"bob-pass","enabled":true}]}`)
	iw := httptest.NewRecorder()
	ireq := httptest.NewRequest(http.MethodPost, "/api/inbounds", inboundBody)
	ireq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(iw, ireq)
	if iw.Code != http.StatusOK && iw.Code != http.StatusCreated {
		t.Fatalf("create inbound: %d %s", iw.Code, iw.Body.String())
	}

	mw := httptest.NewRecorder()
	r.ServeHTTP(mw, httptest.NewRequest(http.MethodPost, "/api/v1/clients/migrate-legacy", strings.NewReader(`{}`)))
	if mw.Code != http.StatusOK {
		t.Fatalf("migrate: %d %s", mw.Code, mw.Body.String())
	}

	// Simulate the interrupted-migration shape: the client and binding rows
	// committed but the credential did not.
	countCredentials := func() int {
		count := -1
		if err := st.clientRepo.WithTx(func(tx *client.Tx) error {
			return tx.QueryRow(`SELECT COUNT(*) FROM client_credentials`).Scan(&count)
		}); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if got := countCredentials(); got < 1 {
		t.Fatalf("initial migration left %d credential rows", got)
	}
	if err := st.clientRepo.WithTx(func(tx *client.Tx) error {
		_, err := tx.Exec(`DELETE FROM client_credentials`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := countCredentials(); got != 0 {
		t.Fatalf("credential teardown left %d rows", got)
	}

	mw2 := httptest.NewRecorder()
	r.ServeHTTP(mw2, httptest.NewRequest(http.MethodPost, "/api/v1/clients/migrate-legacy", strings.NewReader(`{}`)))
	if mw2.Code != http.StatusOK {
		t.Fatalf("repair migrate: %d %s", mw2.Code, mw2.Body.String())
	}
	var mresp2 map[string]any
	if err := json.NewDecoder(mw2.Body).Decode(&mresp2); err != nil {
		t.Fatalf("decode repair migrate: %v", err)
	}
	if created, _ := mresp2["clientsCreated"].(float64); created != 0 {
		t.Fatalf("repair run must not create clients, got %v", created)
	}
	if repaired, _ := mresp2["credentialsCreated"].(float64); repaired < 1 {
		t.Fatalf("repair run reported %v credentialsCreated: %v", repaired, mresp2)
	}
	if got := countCredentials(); got < 1 {
		t.Fatalf("repair run rolled the credential back: %d rows after commit", got)
	}
}
