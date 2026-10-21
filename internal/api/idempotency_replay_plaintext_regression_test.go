package api

import (
	"bytes"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/secrets"
)

// #1283 follow-up: an encrypted=1 replay row whose body is plaintext JSON was
// passed through Cipher.Decrypt verbatim and replayed as a valid one-time
// secret envelope. The replay path must refuse non-"ve1:" bodies with 410.
func TestSecretIdempotencyReplayRefusesPlaintextEnvelope(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	store := newIdempotencyStore(db)
	var key [secrets.KeySize]byte
	digest := sha256.Sum256([]byte("idempotency-plaintext-test-key"))
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
		markIdempotencySecretResponse(w, "c1", 1)
		_, _ = w.Write([]byte(`{"plaintext":"one-time-secret"}`))
	}))
	issue := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/clients/c1/credentials/b1", bytes.NewReader([]byte(`{"kind":"password"}`)))
		r.Header.Set("Idempotency-Key", "issue")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if first := issue(); first.Code != http.StatusOK {
		t.Fatalf("first issuance=%d %s", first.Code, first.Body.String())
	}
	var id string
	var sealed []byte
	if err := db.QueryRow(`SELECT id, response_body FROM idempotency_results WHERE encrypted=1`).Scan(&id, &sealed); err != nil {
		t.Fatal(err)
	}
	// Plant the exact envelope in plaintext: every scope field matches, so
	// only the ciphertext check can stop it.
	envelope, err := store.replayCipher.Decrypt(string(sealed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE idempotency_results SET response_body=? WHERE id=?`, []byte(envelope), id); err != nil {
		t.Fatal(err)
	}
	replay := issue()
	if replay.Code != http.StatusGone || strings.Contains(replay.Body.String(), "one-time-secret") {
		t.Fatalf("plaintext envelope replay=%d body=%s, want 410 without secret", replay.Code, replay.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls=%d, want 1", calls.Load())
	}
}
