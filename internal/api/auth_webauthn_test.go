package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// webAuthnTestHost is the request host every ceremony in this file runs
// under: the relying-party ID derives from it, so the RP stays stable while
// the client IP varies (the per-client throttle would otherwise conflate
// RemoteAddr with RP identity).
const webAuthnTestHost = "panel.test"
const webAuthnTestOrigin = "https://panel.test"

// softPasskey is a test-only ES256 authenticator: it produces real WebAuthn
// assertions/attestations so the login and registration paths exercise the
// actual cryptographic verification, not a stubbed-out library.
type softPasskey struct {
	key          *ecdsa.PrivateKey
	credentialID []byte
	coseKey      []byte
	counter      uint32
}

func newSoftPasskey(t *testing.T) *softPasskey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	credentialID := make([]byte, 32)
	if _, err := rand.Read(credentialID); err != nil {
		t.Fatal(err)
	}
	pubRaw, err := key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	coseKey, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{
			KeyType:   int64(webauthncose.EllipticKey),
			Algorithm: int64(webauthncose.AlgES256),
		},
		Curve:  int64(webauthncose.P256),
		XCoord: pubRaw[1:33],
		YCoord: pubRaw[33:65],
	})
	if err != nil {
		t.Fatal(err)
	}
	return &softPasskey{key: key, credentialID: credentialID, coseKey: coseKey}
}

// storedPasskey renders the authenticator as the persisted model row the
// handlers read back.
func (k *softPasskey) storedPasskey() model.Passkey {
	return model.Passkey{
		ID:        passkeyIDEncoding.EncodeToString(k.credentialID),
		Name:      "test key",
		PublicKey: base64.StdEncoding.EncodeToString(k.coseKey),
		SignCount: k.counter,
	}
}

// authenticatorData builds rpIdHash || flags || signCount. flags is UP(0x01)
// plus AT(0x40) when attested credential data follows.
func authenticatorData(rpID string, flags byte, counter uint32, extra []byte) []byte {
	sum := sha256.Sum256([]byte(rpID))
	data := append([]byte(nil), sum[:]...)
	data = append(data, flags)
	data = binary.BigEndian.AppendUint32(data, counter)
	return append(data, extra...)
}

func clientDataJSON(t *testing.T, ceremonyType, origin string, challenge []byte) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"type":        ceremonyType,
		"challenge":   base64.RawURLEncoding.EncodeToString(challenge),
		"origin":      origin,
		"crossOrigin": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func ecdsaSignASN1(t *testing.T, key *ecdsa.PrivateKey, data []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(data)
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	sig, err := asn1.Marshal(struct{ R, S *big.Int }{r, s})
	if err != nil {
		t.Fatal(err)
	}
	return sig
}

// assertionBody signs a webauthn.get response for the given challenge. The
// userHandle mirrors webAuthnUser.WebAuthnID (SHA-256 of the username).
func (k *softPasskey) assertionBody(t *testing.T, username, rpID, origin string, challenge []byte) []byte {
	t.Helper()
	k.counter++
	authData := authenticatorData(rpID, 0x01, k.counter, nil)
	clientData := clientDataJSON(t, "webauthn.get", origin, challenge)
	clientHash := sha256.Sum256(clientData)
	sig := ecdsaSignASN1(t, k.key, append(authData, clientHash[:]...))
	handle := sha256.Sum256([]byte(username))
	body, err := json.Marshal(map[string]any{
		"id":    base64.RawURLEncoding.EncodeToString(k.credentialID),
		"rawId": base64.RawURLEncoding.EncodeToString(k.credentialID),
		"type":  "public-key",
		"response": map[string]any{
			"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
			"signature":         base64.RawURLEncoding.EncodeToString(sig),
			"userHandle":        base64.RawURLEncoding.EncodeToString(handle[:]),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// attestationBody builds a webauthn.create response with fmt=none attestation:
// attData = aaguid || credIDLen || credID || COSE public key.
func (k *softPasskey) attestationBody(t *testing.T, rpID, origin string, challenge []byte) []byte {
	t.Helper()
	credData := make([]byte, 16) // nil AAGUID
	credData = binary.BigEndian.AppendUint16(credData, uint16(len(k.credentialID)))
	credData = append(credData, k.credentialID...)
	credData = append(credData, k.coseKey...)
	authData := authenticatorData(rpID, 0x41, 0, credData)
	attestation, err := webauthncbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
	})
	if err != nil {
		t.Fatal(err)
	}
	clientData := clientDataJSON(t, "webauthn.create", origin, challenge)
	body, err := json.Marshal(map[string]any{
		"id":    base64.RawURLEncoding.EncodeToString(k.credentialID),
		"rawId": base64.RawURLEncoding.EncodeToString(k.credentialID),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
			"attestationObject": base64.RawURLEncoding.EncodeToString(attestation),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// webauthnTestUser builds a user holding the authenticator's credential.
func webauthnTestUser(t *testing.T, password string, key *softPasskey) User {
	t.Helper()
	user := totpTestUser(t, password)
	user.Passkeys = []model.Passkey{key.storedPasskey()}
	return user
}

func webAuthnPendingLogin(t *testing.T, state *managementState, password string) string {
	t.Helper()
	return cookieValue(totpLogin(t, state, password), pendingSecondFactorCookie)
}

var webauthnCall int

func webauthnRequest(t *testing.T, method, url string, body []byte, pendingToken string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Vary the client IP so the shared (client, username) token bucket does
	// not conflate independent tests; the injected auth clock still steers
	// the exponential backoff.
	webauthnCall++
	req.RemoteAddr = fmt.Sprintf("198.51.100.%d:443", 1+webauthnCall%200)
	if pendingToken != "" {
		req.AddCookie(&http.Cookie{Name: pendingSecondFactorCookie, Value: pendingToken})
	}
	return req
}

// webauthnBegin runs POST /api/v1/auth/webauthn/begin and returns the raw
// assertion options map on success.
func webauthnBegin(t *testing.T, state *managementState, pendingToken string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := webauthnRequest(t, http.MethodPost, "https://"+webAuthnTestHost+"/api/v1/auth/webauthn/begin", nil, pendingToken)
	rec := httptest.NewRecorder()
	state.handleWebAuthnLoginBegin(rec, req)
	if rec.Code != http.StatusOK {
		return rec, nil
	}
	options := decodeBody(t, rec)
	return rec, options
}

func webauthnFinish(t *testing.T, state *managementState, pendingToken string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := webauthnRequest(t, http.MethodPost, "https://"+webAuthnTestHost+"/api/v1/auth/webauthn/finish", body, pendingToken)
	rec := httptest.NewRecorder()
	state.handleWebAuthnLoginFinish(rec, req)
	return rec
}

// webauthnChallenge extracts the base64url challenge bytes from the begin
// response's publicKey map.
func webauthnChallenge(t *testing.T, options map[string]any) []byte {
	t.Helper()
	publicKey, ok := options["publicKey"].(map[string]any)
	if !ok {
		t.Fatalf("assertion options missing publicKey: %v", options)
	}
	encoded, ok := publicKey["challenge"].(string)
	if !ok {
		t.Fatalf("assertion options missing challenge: %v", publicKey)
	}
	challenge, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	return challenge
}

func authedPasskeyRequest(t *testing.T, state *managementState, session Session, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "https://"+webAuthnTestHost+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	req = req.WithContext(context.WithValue(req.Context(), contextKeyUsername, session.Username))
	rec := httptest.NewRecorder()
	switch {
	case path == "/api/v1/users/me/passkeys":
		state.handleMyPasskeys(rec, req)
	case path == "/api/v1/users/me/passkeys/register/begin":
		state.handleMyPasskeyRegisterBegin(rec, req)
	case path == "/api/v1/users/me/passkeys/register/finish":
		state.handleMyPasskeyRegisterFinish(rec, req)
	case strings.HasPrefix(path, "/api/v1/users/me/passkeys/"):
		state.handleMyPasskeyByID(rec, req)
	default:
		t.Fatalf("no handler mapped for %s", path)
	}
	return rec
}

func TestWebAuthnLoginAdvertisesPasskeyFactor(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	state, _ := totpTestState(t, user)

	rec := totpLogin(t, state, "correct-password-123")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	if body["secondFactorRequired"] != true {
		t.Fatalf("expected secondFactorRequired, got %v", body)
	}
	methods, _ := body["secondFactorMethods"].([]any)
	found := false
	for _, method := range methods {
		if method == "webauthn" {
			found = true
		}
	}
	if !found {
		t.Fatalf("secondFactorMethods %v lacks webauthn", methods)
	}
	if cookieValue(rec, "veil_session") != "" {
		t.Fatal("login minted a session before the passkey assertion")
	}
}

func TestWebAuthnLoginBeginFinishCompletesLogin(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	state, _ := totpTestState(t, user)

	pending := webAuthnPendingLogin(t, state, "correct-password-123")
	if pending == "" {
		t.Fatal("no pending_2fa cookie issued")
	}
	_, options := webauthnBegin(t, state, pending)
	challenge := webauthnChallenge(t, options)

	rec := webauthnFinish(t, state, pending, key.assertionBody(t, "alice", webAuthnTestHost, webAuthnTestOrigin, challenge))
	if rec.Code != http.StatusOK {
		t.Fatalf("finish status=%d body=%s", rec.Code, rec.Body.String())
	}
	token := cookieValue(rec, "veil_session")
	if token == "" {
		t.Fatal("finish did not mint the session cookie")
	}
	if cookieValue(rec, pendingSecondFactorCookie) != "" {
		t.Fatal("pending cookie was not cleared")
	}
	session, ok := state.sessionRegistry().Get(token)
	if !ok || !session.SecondFactor {
		t.Fatalf("session missing or unmarked: ok=%v session=%+v", ok, session)
	}
	// The sign counter advanced on the stored credential.
	if got := state.users[0].Passkeys[0].SignCount; got != key.counter {
		t.Fatalf("stored signCount=%d, want %d", got, key.counter)
	}
	// The pending challenge is single-use: replaying the same assertion
	// under the same token fails.
	rec = webauthnFinish(t, state, pending, key.assertionBody(t, "alice", webAuthnTestHost, webAuthnTestOrigin, challenge))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("replayed finish status=%d, want 401", rec.Code)
	}
}

func TestWebAuthnLoginBeginRequiresPendingCookie(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	state, _ := totpTestState(t, user)

	rec, _ := webauthnBegin(t, state, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("begin without pending cookie status=%d, want 401", rec.Code)
	}
}

func TestWebAuthnFinishWithoutBeginFailsClosed(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	state, _ := totpTestState(t, user)

	pending := webAuthnPendingLogin(t, state, "correct-password-123")
	// No begin was run: no ceremony challenge exists for this pending token.
	rec := webauthnFinish(t, state, pending, key.assertionBody(t, "alice", webAuthnTestHost, webAuthnTestOrigin, []byte("bogus")))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("finish without begin status=%d, want 401", rec.Code)
	}
	if cookieValue(rec, "veil_session") != "" {
		t.Fatal("session minted without a ceremony")
	}
}

func TestWebAuthnFinishRejectsWrongOrigin(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	state, _ := totpTestState(t, user)

	pending := webAuthnPendingLogin(t, state, "correct-password-123")
	_, options := webauthnBegin(t, state, pending)
	challenge := webauthnChallenge(t, options)
	// A valid signature over a mismatched origin must still fail closed.
	body := key.assertionBody(t, "alice", webAuthnTestHost, "https://evil.example", challenge)
	rec := webauthnFinish(t, state, pending, body)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("foreign-origin assertion status=%d, want 401", rec.Code)
	}
	if cookieValue(rec, "veil_session") != "" {
		t.Fatal("session minted for a foreign origin")
	}
}

func TestWebAuthnFinishRejectsUnknownCredential(t *testing.T) {
	key := newSoftPasskey(t)
	stranger := newSoftPasskey(t) // never registered to alice
	user := webauthnTestUser(t, "correct-password-123", key)
	state, _ := totpTestState(t, user)

	pending := webAuthnPendingLogin(t, state, "correct-password-123")
	_, options := webauthnBegin(t, state, pending)
	challenge := webauthnChallenge(t, options)
	rec := webauthnFinish(t, state, pending, stranger.assertionBody(t, "alice", webAuthnTestHost, webAuthnTestOrigin, challenge))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown credential status=%d, want 401", rec.Code)
	}
}

func TestWebAuthnSignCountRegressionInvalidatesCredential(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	state, _ := totpTestState(t, user)

	pending := webAuthnPendingLogin(t, state, "correct-password-123")
	_, options := webauthnBegin(t, state, pending)
	challenge := webauthnChallenge(t, options)
	rec := webauthnFinish(t, state, pending, key.assertionBody(t, "alice", webAuthnTestHost, webAuthnTestOrigin, challenge))
	if rec.Code != http.StatusOK {
		t.Fatalf("first finish status=%d body=%s", rec.Code, rec.Body.String())
	}

	// A cloned authenticator replays an OLD counter: the login must fail and
	// the credential must be deleted from the account. assertionBody
	// increments first, so counter=0 presents 1 — equal to the stored 1.
	pending = webAuthnPendingLogin(t, state, "correct-password-123")
	_, options = webauthnBegin(t, state, pending)
	challenge = webauthnChallenge(t, options)
	key.counter = 0
	rec = webauthnFinish(t, state, pending, key.assertionBody(t, "alice", webAuthnTestHost, webAuthnTestOrigin, challenge))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("regressed-counter assertion status=%d, want 401", rec.Code)
	}
	if got := state.users[0].Passkeys; len(got) != 0 {
		t.Fatalf("cloned credential survived: %v", got)
	}
	if cookieValue(rec, "veil_session") != "" {
		t.Fatal("session minted for a cloned credential")
	}
}

func TestWebAuthnChallengeStoreExpiresAndSingleUse(t *testing.T) {
	now := time.Now()
	store := newWebAuthnChallengeStore(func() time.Time { return now })
	store.Put("token-a", "alice", webauthn.SessionData{Challenge: "AAAA"})
	if _, ok := store.Take("token-b"); ok {
		t.Fatal("unknown key resolved")
	}
	entry, ok := store.Take("token-a")
	if !ok || entry.Username != "alice" {
		t.Fatalf("fresh challenge not returned: %+v ok=%v", entry, ok)
	}
	if _, ok := store.Take("token-a"); ok {
		t.Fatal("consumed challenge resolved a second time")
	}
	store.Put("token-c", "alice", webauthn.SessionData{Challenge: "BBBB"})
	now = now.Add(webAuthnChallengeTTL + time.Second)
	if _, ok := store.Take("token-c"); ok {
		t.Fatal("expired challenge resolved")
	}
}

func TestWebAuthnPendingExpiry(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	state, now := totpTestState(t, user)

	pending := webAuthnPendingLogin(t, state, "correct-password-123")
	*now = now.Add(pendingSecondFactorTTL + time.Second)
	rec, _ := webauthnBegin(t, state, pending)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("begin on expired pending status=%d, want 401", rec.Code)
	}
}

func TestWebAuthnRegistrationLifecycle(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	state, _ := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	key := newSoftPasskey(t)

	// Begin requires the account password while the session lacks the mark.
	rec := authedPasskeyRequest(t, state, session, http.MethodPost, "/api/v1/users/me/passkeys/register/begin", []byte(`{"name":"laptop"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("begin without password status=%d, want 400", rec.Code)
	}
	rec = authedPasskeyRequest(t, state, session, http.MethodPost, "/api/v1/users/me/passkeys/register/begin", []byte(`{"password":"wrong","name":"laptop"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("begin with bad password status=%d, want 400", rec.Code)
	}
	// Step past the credential-gate backoff before the good attempt.
	state.loginBackoffNow = func() time.Time { return time.Now().Add(time.Hour) }
	rec = authedPasskeyRequest(t, state, session, http.MethodPost, "/api/v1/users/me/passkeys/register/begin", []byte(`{"password":"correct-password-123","name":"laptop"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("begin status=%d body=%s", rec.Code, rec.Body.String())
	}
	options := decodeBody(t, rec)
	publicKey, _ := options["publicKey"].(map[string]any)
	if publicKey == nil {
		t.Fatalf("creation options missing publicKey: %v", options)
	}
	encoded, _ := publicKey["challenge"].(string)
	challenge, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode creation challenge: %v", err)
	}

	// Finish verifies the attestation and stores the credential.
	finishBody, _ := json.Marshal(map[string]any{
		"name":       "laptop",
		"credential": json.RawMessage(key.attestationBody(t, webAuthnTestHost, webAuthnTestOrigin, challenge)),
	})
	rec = authedPasskeyRequest(t, state, session, http.MethodPost, "/api/v1/users/me/passkeys/register/finish", finishBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("finish status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(state.users[0].Passkeys) != 1 {
		t.Fatalf("passkey not stored: %+v", state.users[0].Passkeys)
	}
	// Registering a factor upgrades the enrolling session (#1172 pattern).
	marked, ok := state.sessionRegistry().Get(session.Token)
	if !ok || !marked.SecondFactor {
		t.Fatalf("registering session not upgraded: %+v", marked)
	}

	// The list endpoint exposes only public metadata.
	rec = authedPasskeyRequest(t, state, session, http.MethodGet, "/api/v1/users/me/passkeys", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	entries, _ := body["passkeys"].([]any)
	if len(entries) != 1 {
		t.Fatalf("list=%v", body)
	}
	entry, _ := entries[0].(map[string]any)
	if entry["id"] != key.storedPasskey().ID || entry["name"] != "laptop" {
		t.Fatalf("list entry=%v", entry)
	}
	if _, leaked := entry["publicKey"]; leaked {
		t.Fatalf("list leaked public key material: %v", entry)
	}

	// The passkey is the account's ONLY second factor, so its removal is
	// factor-grade (#1232): even a marked session must re-present the
	// account password — the mark alone cannot lower the login floor.
	rec = authedPasskeyRequest(t, state, session, http.MethodDelete, "/api/v1/users/me/passkeys/"+key.storedPasskey().ID, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("last-factor delete without password status=%d, want 400", rec.Code)
	}
	if len(state.users[0].Passkeys) != 1 {
		t.Fatal("last passkey deleted without a password re-entry")
	}
	rec = authedPasskeyRequest(t, state, session, http.MethodDelete, "/api/v1/users/me/passkeys/"+key.storedPasskey().ID, []byte(`{"password":"correct-password-123"}`))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(state.users[0].Passkeys) != 0 {
		t.Fatalf("delete left passkeys: %v", state.users[0].Passkeys)
	}
	// Disarming the account revoked every OTHER session; the one that
	// proved the password survives (#1232, #1171 revocation pattern).
	if _, ok := state.sessionRegistry().Get(session.Token); !ok {
		t.Fatal("the deleting session was revoked with the last factor")
	}
}

func TestWebAuthnAdminResetClearsCredentials(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	// TOTP stays armed through the reset, so the login floor cannot drop:
	// a live user session must survive (#1171). The sole-factor case that
	// revokes sessions is covered by
	// TestWebAuthnAdminResetRevokesSessionsWhenSoleFactor (#1232).
	user.TOTPEnabled = true
	user.TOTPSecret = "JBSWY3DPEHPK3PXP"
	state, _ := totpTestState(t, user)
	userSession := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/alice/passkeys", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()
	state.handleV1UserFactorReset(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin reset status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(state.users[0].Passkeys) != 0 {
		t.Fatalf("reset left passkeys: %v", state.users[0].Passkeys)
	}
	if _, ok := state.sessionRegistry().Get(userSession.Token); !ok {
		t.Fatal("passkey reset revoked the user's session")
	}

	// Idempotent on a clean account.
	rec = httptest.NewRecorder()
	state.handleV1UserFactorReset(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("idempotent reset status=%d", rec.Code)
	}

	// Viewers cannot reset factors.
	state.users[0].Passkeys = []model.Passkey{key.storedPasskey()}
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/users/alice/passkeys", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "viewer"))
	rec = httptest.NewRecorder()
	state.handleV1UserFactorReset(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer reset status=%d, want 403", rec.Code)
	}
}

func TestUpdateUserPreservesPasskeys(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	state, _ := totpTestState(t, user)

	// A generic role/locale update carrying a FORGED passkey list must not
	// smuggle credential changes: UpdateUser preserves the stored list and
	// writes go through SetUserPasskeys only.
	forged := newSoftPasskey(t)
	var updated model.User
	err := state.withMutation(func(mutation managementstate.Mutation) error {
		var mErr error
		updated, mErr = mutation.UpdateUser("alice", model.User{
			Username:     "alice",
			Role:         "admin",
			Locale:       "ru",
			PasswordHash: state.users[0].PasswordHash,
			Passkeys:     []model.Passkey{forged.storedPasskey()},
		})
		return mErr
	})
	if err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}
	if len(updated.Passkeys) != 1 || updated.Passkeys[0].ID != key.storedPasskey().ID {
		t.Fatalf("UpdateUser overwrote passkeys: %+v", updated.Passkeys)
	}
	if state.users[0].Passkeys[0].ID != key.storedPasskey().ID {
		t.Fatalf("stored passkey replaced by forged credential")
	}

	// SetUserPasskeys remains the dedicated write path.
	err = state.withMutation(func(mutation managementstate.Mutation) error {
		cleared := state.users[0]
		cleared.ClearPasskeys()
		_, mErr := mutation.SetUserPasskeys("alice", cleared)
		return mErr
	})
	if err != nil {
		t.Fatalf("SetUserPasskeys failed: %v", err)
	}
	if len(state.users[0].Passkeys) != 0 {
		t.Fatalf("SetUserPasskeys left passkeys: %v", state.users[0].Passkeys)
	}
}

func TestPasskeyArmsSecondFactorRequirement(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	state, _ := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	// A passkey-only account arms the factor requirement the same as TOTP:
	// sessions minted before the credential was registered are retired.
	if state.sessionMeetsFactorRequirement(session) {
		t.Fatal("pre-enrollment session accepted after passkey was armed")
	}
	state.mu.Lock()
	state.users[0].Passkeys = nil
	state.mu.Unlock()
	if !state.sessionMeetsFactorRequirement(session) {
		t.Fatal("session still required the mark after the last factor was removed")
	}
}

// Deleting the account's LAST second factor is factor-grade (#1232): the
// second-factor session mark alone must not downgrade a passkey-only
// account to password-only login — the account password is required, and
// every other session dies with the factor (TOTP-disable's contract).
func TestMyPasskeysDeleteLastFactorRequiresPasswordAndRevokesSessions(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	state, _ := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	other := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	if _, err := state.sessionRegistry().MarkSecondFactorPersisted(session.Token); err != nil {
		t.Fatalf("mark session: %v", err)
	}
	if _, err := state.sessionRegistry().MarkSecondFactorPersisted(other.Token); err != nil {
		t.Fatalf("mark other session: %v", err)
	}
	path := "/api/v1/users/me/passkeys/" + key.storedPasskey().ID

	// Even the 2FA-complete session mark is insufficient for last-factor
	// removal — a stolen session must not downgrade the account.
	rec := authedPasskeyRequest(t, state, session, http.MethodDelete, path, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("last-factor delete on marked session status=%d, want 400", rec.Code)
	}
	if len(state.users[0].Passkeys) != 1 {
		t.Fatal("session mark alone deleted the last factor")
	}
	rec = authedPasskeyRequest(t, state, session, http.MethodDelete, path, []byte(`{"password":"wrong-password"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("last-factor delete with bad password status=%d, want 400", rec.Code)
	}
	// Step past the credential-gate backoff before the good attempt.
	state.loginBackoffNow = func() time.Time { return time.Now().Add(time.Hour) }
	rec = authedPasskeyRequest(t, state, session, http.MethodDelete, path, []byte(`{"password":"correct-password-123"}`))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("last-factor delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(state.users[0].Passkeys) != 0 {
		t.Fatalf("delete left passkeys: %v", state.users[0].Passkeys)
	}
	// The factor is gone: every session except the one that proved the
	// password must be revoked with it.
	if _, ok := state.sessionRegistry().Get(other.Token); ok {
		t.Fatal("sibling session survived the last-factor deletion")
	}
	if _, ok := state.sessionRegistry().Get(session.Token); !ok {
		t.Fatal("the deleting session was revoked with the last factor")
	}
}

// Removing one of SEVERAL passkeys keeps the lighter contract (#1171): a
// second-factor-marked session deletes without re-presenting the password
// and no sessions are revoked, because the account still has a factor.
func TestMyPasskeysDeleteNonLastFactorKeepsSessionMarkPath(t *testing.T) {
	first, second := newSoftPasskey(t), newSoftPasskey(t)
	user := totpTestUser(t, "correct-password-123")
	user.Passkeys = []model.Passkey{first.storedPasskey(), second.storedPasskey()}
	state, _ := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	if _, err := state.sessionRegistry().MarkSecondFactorPersisted(session.Token); err != nil {
		t.Fatalf("mark session: %v", err)
	}

	rec := authedPasskeyRequest(t, state, session, http.MethodDelete, "/api/v1/users/me/passkeys/"+first.storedPasskey().ID, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("non-last delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(state.users[0].Passkeys) != 1 || state.users[0].Passkeys[0].ID != second.storedPasskey().ID {
		t.Fatalf("wrong credential removed: %+v", state.users[0].Passkeys)
	}
	if _, ok := state.sessionRegistry().Get(session.Token); !ok {
		t.Fatal("non-last-factor delete revoked the session")
	}
}

// An admin reset that strips the account's LAST factor permanently lowers
// the login floor to password-only — every session the target holds must
// be revoked with it, the same contract TOTP reset upholds (#1232).
func TestWebAuthnAdminResetRevokesSessionsWhenSoleFactor(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	state, _ := totpTestState(t, user)
	first := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")
	second := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/users/alice/passkeys", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextKeyRole, "admin"))
	rec := httptest.NewRecorder()
	state.handleV1UserFactorReset(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin reset status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(state.users[0].Passkeys) != 0 {
		t.Fatalf("reset left passkeys: %v", state.users[0].Passkeys)
	}
	for _, token := range []string{first.Token, second.Token} {
		if _, ok := state.sessionRegistry().Get(token); ok {
			t.Fatal("target session survived the last-factor reset")
		}
	}
}

// With a configured domain the RP ID is pinned to it and the request Host
// must sit inside that domain tree — an unrelated Host is refused rather
// than silently minting credentials bound to an attacker-controlled RP
// (#1222). Without configuration the Host fallback still serves.
func TestWebAuthnRPPinsConfiguredDomainAndValidatesHost(t *testing.T) {
	user := totpTestUser(t, "correct-password-123")
	state, _ := totpTestState(t, user)
	state.settings.Domain = "panel.example.com"

	req := httptest.NewRequest(http.MethodPost, "https://panel.example.com/api/v1/auth/webauthn/begin", nil)
	wa, rpID, err := state.webAuthnForRequest(req)
	if err != nil || rpID != "panel.example.com" {
		t.Fatalf("configured-domain rpID=%q err=%v wa=%v", rpID, err, wa)
	}
	// A subdomain of the configured domain is admitted.
	req = httptest.NewRequest(http.MethodPost, "https://deep.panel.example.com:8443/api/v1/auth/webauthn/begin", nil)
	if _, rpID, err = state.webAuthnForRequest(req); err != nil || rpID != "panel.example.com" {
		t.Fatalf("subdomain rpID=%q err=%v", rpID, err)
	}
	// An unrelated — or lookalike-suffix — Host is refused.
	for _, host := range []string{"evil.example", "panel.example.com.evil.example"} {
		req = httptest.NewRequest(http.MethodPost, "https://"+host+"/api/v1/auth/webauthn/begin", nil)
		if _, _, err = state.webAuthnForRequest(req); err == nil {
			t.Fatalf("foreign host %q configured an RP", host)
		}
	}
	// PanelDomain under caddy access wins over the site domain.
	state.settings.PanelAccess = "caddy"
	state.settings.PanelDomain = "dash.example.org"
	req = httptest.NewRequest(http.MethodPost, "https://dash.example.org/api/v1/auth/webauthn/begin", nil)
	if _, rpID, err = state.webAuthnForRequest(req); err != nil || rpID != "dash.example.org" {
		t.Fatalf("panel-domain rpID=%q err=%v", rpID, err)
	}
	req = httptest.NewRequest(http.MethodPost, "https://panel.example.com/api/v1/auth/webauthn/begin", nil)
	if _, _, err = state.webAuthnForRequest(req); err == nil {
		t.Fatal("site-domain host admitted while the caddy panel domain is configured")
	}
	// No configured domain: the request Host is the RP ID fallback.
	state.settings.Domain = ""
	state.settings.PanelAccess = ""
	state.settings.PanelDomain = ""
	req = httptest.NewRequest(http.MethodPost, "https://direct.test:9443/api/v1/auth/webauthn/begin", nil)
	if _, rpID, err = state.webAuthnForRequest(req); err != nil || rpID != "direct.test" {
		t.Fatalf("host-fallback rpID=%q err=%v", rpID, err)
	}
}

// Every factor ceremony response carries secret material (the single-use
// challenge or a session-minting body), so the idempotency store must file
// it secret-grade — encrypted, 5-minute replay TTL (#1221).
func TestWebAuthnCeremonyResponsesMarkSecretIdempotentResponses(t *testing.T) {
	key := newSoftPasskey(t)
	user := webauthnTestUser(t, "correct-password-123", key)
	state, _ := totpTestState(t, user)
	session := mustCreateSession(t, state.sessionRegistry(), "alice", "admin")

	store := newIdempotencyStore()
	defer store.Close()
	count := func() int {
		store.mu.Lock()
		defer store.mu.Unlock()
		secret := 0
		for _, entry := range store.entries {
			if entry.secret {
				secret++
			}
		}
		return secret
	}

	// Passkey registration begin — the creation challenge.
	beginRegister := store.Middleware(http.HandlerFunc(state.handleMyPasskeyRegisterBegin))
	req := httptest.NewRequest(http.MethodPost, "https://"+webAuthnTestHost+"/api/v1/users/me/passkeys/register/begin",
		bytes.NewReader([]byte(`{"password":"correct-password-123","name":"x"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "register-begin")
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	req = req.WithContext(context.WithValue(req.Context(), contextKeyUsername, "alice"))
	rec := httptest.NewRecorder()
	beginRegister.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register begin status=%d body=%s", rec.Code, rec.Body.String())
	}
	if count() != 1 {
		t.Fatalf("register begin secret entries=%d, want 1", count())
	}

	// WebAuthn login begin — the assertion challenge.
	pending := webAuthnPendingLogin(t, state, "correct-password-123")
	beginLogin := store.Middleware(http.HandlerFunc(state.handleWebAuthnLoginBegin))
	req = webauthnRequest(t, http.MethodPost, "https://"+webAuthnTestHost+"/api/v1/auth/webauthn/begin", nil, pending)
	req.Header.Set("Idempotency-Key", "login-begin")
	rec = httptest.NewRecorder()
	beginLogin.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login begin status=%d body=%s", rec.Code, rec.Body.String())
	}
	if count() != 2 {
		t.Fatalf("login begin secret entries=%d, want 2", count())
	}
}
