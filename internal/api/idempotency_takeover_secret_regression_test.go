package api

import (
	"bytes"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/secrets"
)

func TestStaleIdempotencyReservationTakeoverFencesOldOwner(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	now := time.Unix(1_900_000_000, 0).UTC()
	oldStore := newIdempotencyStore(db)
	oldStore.now = func() time.Time { return now }
	oldStore.owner = "old-owner"
	request := httptest.NewRequest(http.MethodPost, "/api/clients", strings.NewReader(`{"name":"one"}`))
	request.Header.Set("Idempotency-Key", "same")
	body := []byte(`{"name":"one"}`)
	scope := idempotencyScope(request, "same")
	fingerprint := idempotencyFingerprint(request, body)
	owned, oldRecord, err := oldStore.reserveDurable(request, "same", scope, fingerprint)
	if err != nil || !owned || oldRecord.Generation != 1 {
		t.Fatalf("initial reservation: owned=%v record=%+v err=%v", owned, oldRecord, err)
	}
	if _, err := db.Exec(`UPDATE idempotency_records SET reserved_until=? WHERE scope=?`, now.Add(-time.Second).Unix(), scope); err != nil {
		t.Fatal(err)
	}
	newStore := newIdempotencyStore(db)
	newStore.now = func() time.Time { return now }
	newStore.owner = "new-owner"
	owned, newRecord, err := newStore.reserveDurable(request, "same", scope, fingerprint)
	if err != nil || !owned || newRecord.Generation != 2 {
		t.Fatalf("takeover: owned=%v record=%+v err=%v", owned, newRecord, err)
	}
	if err := oldStore.completeDurable(scope, fingerprint, oldRecord, http.StatusCreated, http.Header{}, []byte("old"), "committed"); err == nil {
		t.Fatal("old owner completed after generation takeover")
	}
	if err := newStore.completeDurable(scope, fingerprint, newRecord, http.StatusCreated, http.Header{}, []byte("new"), "committed"); err != nil {
		t.Fatalf("new owner completion: %v", err)
	}
	persisted, err := newStore.readDurable(scope)
	if err != nil || persisted.State != "completed" || string(persisted.Body) != "new" {
		t.Fatalf("persisted takeover result=%+v err=%v", persisted, err)
	}
}

func TestSecretIdempotencyReplayIsEncryptedAndInvalidatedByRotate(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	store := newIdempotencyStore(db)
	var key [secrets.KeySize]byte
	digest := sha256.Sum256([]byte("idempotency-test-key"))
	copy(key[:], digest[:])
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.setReplayCipher(cipher); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		generation := uint64(1)
		if strings.HasSuffix(r.URL.Path, "/rotate") {
			generation = 2
		}
		markIdempotencySecretResponse(w, "c1", generation)
		_, _ = w.Write([]byte(`{"plaintext":"one-time-secret"}`))
	}))
	issue := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/clients/c1/credentials/b1", bytes.NewReader([]byte(`{"kind":"password"}`)))
		r.Header.Set("Idempotency-Key", "issue")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	first := issue()
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "one-time-secret") {
		t.Fatalf("first issuance=%d %s", first.Code, first.Body.String())
	}
	var stored []byte
	var encrypted int
	if err := db.QueryRow(`SELECT response_body,encrypted FROM idempotency_results WHERE encrypted=1`).Scan(&stored, &encrypted); err != nil {
		t.Fatal(err)
	}
	if encrypted != 1 || bytes.Contains(stored, []byte("one-time-secret")) {
		t.Fatalf("plaintext secret persisted: encrypted=%d body=%q", encrypted, stored)
	}
	replay := issue()
	if replay.Code != http.StatusOK || replay.Header().Get("Idempotency-Replayed") != "true" || !strings.Contains(replay.Body.String(), "one-time-secret") || calls.Load() != 1 {
		t.Fatalf("encrypted replay=%d headers=%v body=%s calls=%d", replay.Code, replay.Header(), replay.Body.String(), calls.Load())
	}
	rotate := httptest.NewRequest(http.MethodPost, "/api/v1/clients/c1/credentials/b1/rotate", bytes.NewReader([]byte(`{"kind":"password"}`)))
	rotate.Header.Set("Idempotency-Key", "rotate")
	rotateResponse := httptest.NewRecorder()
	handler.ServeHTTP(rotateResponse, rotate)
	if rotateResponse.Code != http.StatusOK {
		t.Fatalf("rotate=%d %s", rotateResponse.Code, rotateResponse.Body.String())
	}
	invalidated := issue()
	if invalidated.Code != http.StatusGone || calls.Load() != 2 {
		t.Fatalf("revoked secret replay=%d body=%s calls=%d", invalidated.Code, invalidated.Body.String(), calls.Load())
	}
}

func TestOversizedPostCommitResultDoesNotRepeatMutation(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	store := newIdempotencyStore(db)
	var calls atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxIdempotencyBody+1))
	}))
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/large", nil)
		r.Header.Set("Idempotency-Key", "large")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	first := request()
	second := request()
	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted || calls.Load() != 1 || first.Body.String() != second.Body.String() || !strings.Contains(second.Body.String(), "response_too_large") {
		t.Fatalf("first=%d retry=%d firstBody=%s retryBody=%s calls=%d", first.Code, second.Code, first.Body.String(), second.Body.String(), calls.Load())
	}
}
