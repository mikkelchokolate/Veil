package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// #1106: unauthenticated OPTIONS must never reach an application handler.
// Registered paths get a uniform 204 + Allow; unknown /api and /s/ paths fail
// closed with the same 404 every other unregistered route gets — no
// handler-specific 200/403/404 protocol or existence oracle leaks.
func TestUnauthenticatedOptionsNeverReachHandlers(t *testing.T) {
	var invoked atomic.Int32
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		invoked.Add(1)
		w.WriteHeader(http.StatusTeapot) // would leak handler presence if reached
	})
	state := &managementState{sessions: mustNewSessionRegistry("")}
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{AllowDevAnonymous: true}, next)

	for _, path := range []string{
		"/api/inbounds",
		"/api/auth/login",
		"/api/setup/status",
		"/api/apply/rollback",
		"/api/v1/clients/x/tokens",
		// A parameterized pattern that matches only mutating methods still
		// resolves to a registered route: the uniform 204 reveals nothing
		// about whether the concrete resource ("x") exists — the Allow set
		// is pattern-level, identical for every parameter value.
		"/api/users/x",
		"/s/some-token",
		"/metrics",
		"/healthz",
	} {
		req := httptest.NewRequest(http.MethodOptions, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("OPTIONS %s: status=%d, want 204 (handler must not run)", path, rec.Code)
		}
		if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "OPTIONS") {
			t.Fatalf("OPTIONS %s: missing Allow header, got %q", path, allow)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("OPTIONS %s: unexpected body %q", path, rec.Body.String())
		}
	}
	// OPTIONS on a path that registers no methods inside the protected
	// namespaces is an unknown endpoint — fail closed like any other
	// unregistered API route.
	for _, path := range []string{"/api/does-not-exist", "/api", "/s", "/api/users/x/extra"} {
		req := httptest.NewRequest(http.MethodOptions, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("OPTIONS %s: status=%d, want 404", path, rec.Code)
		}
	}
	// OPTIONS to a multi-method path advertises every registered method.
	req := httptest.NewRequest(http.MethodOptions, "/api/inbounds", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	allow := rec.Header().Get("Allow")
	for _, method := range []string{"GET", "POST", "OPTIONS"} {
		if !strings.Contains(allow, method) {
			t.Fatalf("OPTIONS /api/inbounds Allow=%q missing %s", allow, method)
		}
	}
	if got := invoked.Load(); got != 0 {
		t.Fatalf("application handler invoked %d times by OPTIONS", got)
	}
}

// #1106: OPTIONS must not inherit another method's capability — the blanket
// public classification is gone.
func TestOptionsIsNotBlanketPublic(t *testing.T) {
	for _, path := range []string{"/api/inbounds", "/api/auth/login", "/s/tok"} {
		if capability, known := capabilityForEndpoint(http.MethodOptions, path); known && capability == capabilityPublic {
			// Only legitimately public destinations may remain public.
			if path != "/s/tok" {
				t.Fatalf("OPTIONS %s still classified public", path)
			}
		}
	}
	// /api/auth/login is POST-only in the catalog: OPTIONS there must not
	// surface the public capability at all (it escalates to the conservative
	// admin gate, which anonymous callers cannot satisfy).
	capability, _ := capabilityForEndpoint(http.MethodOptions, "/api/auth/login")
	if capability == capabilityPublic {
		t.Fatal("OPTIONS /api/auth/login resolves to public capability")
	}
}

// #1105: a single account can hold at most maxSessionsPerUser sessions;
// overflow evicts that user's OWN oldest sessions — never another user's.
func TestPerUserSessionCapEvictsOnlyOwnSessions(t *testing.T) {
	registry := mustNewSessionRegistry("")
	admin, err := registry.Create(SessionCreateInput{Username: "admin", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	firstViewer, err := registry.Create(SessionCreateInput{Username: "viewer", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxSessionsPerUser; i++ {
		if _, err := registry.Create(SessionCreateInput{Username: "viewer", Role: "viewer"}); err != nil {
			t.Fatalf("viewer login %d: %v", i, err)
		}
	}
	if _, ok := registry.Get(admin.Token); !ok {
		t.Fatal("viewer login flood evicted the admin session")
	}
	if _, ok := registry.Get(firstViewer.Token); ok {
		t.Fatal("viewer's own oldest session was not evicted at the per-user cap")
	}
	count := 0
	for _, info := range registry.List("") {
		if info.Username == "viewer" {
			count++
		}
	}
	if count != maxSessionsPerUser {
		t.Fatalf("viewer session count=%d, want %d", count, maxSessionsPerUser)
	}
}

// #1105: at the global bound the fullest account sheds sessions first, and
// privilege-aware selection prefers evicting non-admin sessions — an
// attacker flooding sessions under many names cannot shed an administrator
// merely for having the oldest session.
func TestGlobalSessionCapPrefersNonAdminEviction(t *testing.T) {
	registry := mustNewSessionRegistry("")
	registry.absoluteTimeout = time.Hour
	// One long-lived admin session created first (globally oldest).
	admin, err := registry.Create(SessionCreateInput{Username: "admin", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	// Fill the registry to the global bound with distinct non-admin users —
	// the worst case for "oldest first" eviction.
	flood := maxActiveSessions - 1
	first := make([]Session, 0, 4)
	for i := 0; i < flood; i++ {
		session, err := registry.Create(SessionCreateInput{Username: "flood-" + strconv.Itoa(i), Role: "viewer"})
		if err != nil {
			t.Fatalf("flood create %d: %v", i, err)
		}
		if i < 4 {
			first = append(first, session)
		}
	}
	// One more distinct user crosses the global bound: the busiest-bucket
	// rule sees a uniform tie and the non-admin preference must shed the
	// oldest VIEWER session — never the admin's oldest one.
	if _, err := registry.Create(SessionCreateInput{Username: "attacker-final", Role: "viewer"}); err != nil {
		t.Fatalf("final create: %v", err)
	}
	if _, ok := registry.Get(admin.Token); !ok {
		t.Fatal("global cap evicted the only admin session while viewer sessions existed")
	}
	if _, ok := registry.Get(first[0].Token); ok {
		t.Fatal("global cap did not evict the oldest non-admin session")
	}
}

// #1100: once any panel user exists the instance is provisioned forever —
// rolling back to a pre-setup revision (or restoring a zero-user snapshot)
// must fail closed with 401 for anonymous callers, not re-arm dev-anonymous
// admin. Recovery is `veil admin reset`/`veil admin set`, so the error says so.
func TestRollbackToZeroUserRevisionFailsClosedAnonymous(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)

	// Anonymous (dev-anonymous) mutation — allowed only because the instance
	// was never provisioned.
	created := postJSON(t, router, "/api/inbounds",
		`{"name":"rb-inbound","protocol":"hysteria2","transport":"udp","port":14432,"enabled":true}`)
	if created.Code != http.StatusCreated && created.Code != http.StatusOK {
		t.Fatalf("anonymous create inbound: %d %s", created.Code, created.Body.String())
	}
	firstRev, err := state.applyRevisions.Get()
	if err != nil {
		t.Fatal(err)
	}

	// Create the first admin user — this latches provisioning forever and
	// writes the durable marker beside state.json.
	userCreated := postJSON(t, router, "/api/users",
		`{"username":"admin","password":"a-long-secure-password","role":"admin"}`)
	if userCreated.Code != http.StatusCreated {
		t.Fatalf("create admin: %d %s", userCreated.Code, userCreated.Body.String())
	}
	marker := filepath.Join(filepath.Dir(state.statePath), usersProvisionedMarkerName)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("provisioning marker was not persisted: %v", err)
	}

	// Log in as the real admin (dev-anonymous no longer applies).
	login := postJSON(t, router, "/api/auth/login",
		`{"username":"admin","password":"a-long-secure-password"}`)
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	var loginBody struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &loginBody); err != nil || loginBody.CSRFToken == "" {
		t.Fatalf("login body: %v %s", err, login.Body.String())
	}
	sessionCookie := ""
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == "veil_session" {
			sessionCookie = cookie.Value
		}
	}
	if sessionCookie == "" {
		t.Fatal("login did not set a session cookie")
	}

	// Roll back to the pre-user revision — the exact attacker path from the
	// issue. This is an authenticated admin action, so it must succeed; what
	// must change is that anonymous callers stay locked out afterwards.
	rollbackReq := httptest.NewRequest(http.MethodPost, "/api/apply/rollback",
		strings.NewReader(`{"selectedRevision":`+strconv.FormatUint(firstRev.Desired, 10)+`,"confirm":true}`))
	rollbackReq.Header.Set("Content-Type", "application/json")
	rollbackReq.AddCookie(&http.Cookie{Name: "veil_session", Value: sessionCookie})
	rollbackReq.Header.Set("X-CSRF-Token", loginBody.CSRFToken)
	rollback := httptest.NewRecorder()
	router.ServeHTTP(rollback, rollbackReq)
	if rollback.Code != http.StatusOK {
		t.Fatalf("rollback: %d %s", rollback.Code, rollback.Body.String())
	}
	state.mu.Lock()
	usersAfter := len(state.users)
	state.mu.Unlock()
	if usersAfter != 0 {
		t.Fatalf("rollback did not restore zero users: %d", usersAfter)
	}

	// The anonymous request that was dev-anonymous admin before MUST now be
	// 401 — with the recovery hint — not 200 as admin.
	anon := httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	anonRec := httptest.NewRecorder()
	router.ServeHTTP(anonRec, anon)
	if anonRec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous after rollback to zero users: %d %s, want 401 (dev-anonymous re-armed itself)",
			anonRec.Code, anonRec.Body.String())
	}
	if !strings.Contains(anonRec.Body.String(), "veil admin") {
		t.Fatalf("401 body missing CLI recovery hint: %s", anonRec.Body.String())
	}
	// The pre-rollback admin session must be dead too: its user vanished.
	stale := httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	stale.AddCookie(&http.Cookie{Name: "veil_session", Value: sessionCookie})
	staleRec := httptest.NewRecorder()
	router.ServeHTTP(staleRec, stale)
	if staleRec.Code != http.StatusUnauthorized {
		t.Fatalf("orphaned admin session after rollback: %d, want 401", staleRec.Code)
	}
	// /api/auth/status must not report dev-anonymous admin either.
	status := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, status)
	if statusRec.Code != http.StatusOK || !strings.Contains(statusRec.Body.String(), `"authenticated":false`) {
		t.Fatalf("auth status after rollback: %d %s", statusRec.Code, statusRec.Body.String())
	}
	// First-run setup must stay closed on a provisioned instance.
	setup := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
	setupRec := httptest.NewRecorder()
	router.ServeHTTP(setupRec, setup)
	if setupRec.Code != http.StatusOK || strings.Contains(setupRec.Body.String(), `"required":true`) {
		t.Fatalf("setup status after rollback re-armed first-run: %d %s", setupRec.Code, setupRec.Body.String())
	}
	// And POST /api/setup/complete must fail closed as well. This router
	// runs AllowSetup=false, so the middleware gates the endpoint behind
	// admin auth and anonymous callers get 401 before the handler runs;
	// what matters is it can never succeed.
	complete := postJSON(t, router, "/api/setup/complete",
		`{"username":"admin2","password":"another-secure-password","backupAcknowledged":true}`)
	if complete.Code == http.StatusOK || complete.Code == http.StatusCreated {
		t.Fatalf("setup complete succeeded on provisioned instance: %d %s", complete.Code, complete.Body.String())
	}
	if complete.Code != http.StatusUnauthorized && complete.Code != http.StatusForbidden && complete.Code != http.StatusConflict {
		t.Fatalf("setup complete on provisioned instance: %d %s, want 401/403/409", complete.Code, complete.Body.String())
	}

	// The handler's own latch check is the second line of defence on
	// deployments where setup IS allowed (AllowSetup=true): a
	// provisioned-then-emptied instance gets 409, never a re-provisioned
	// account.
	latchedState := &managementState{
		setupAllowed:     true,
		usersEverExisted: true,
		sessions:         mustNewSessionRegistry(""),
		passwordHasher:   bcryptPasswordHasher{cost: bcrypt.MinCost},
	}
	directReq := httptest.NewRequest(http.MethodPost, "/api/setup/complete",
		strings.NewReader(`{"username":"admin2","password":"another-secure-password","backupAcknowledged":true}`))
	directReq.Header.Set("Content-Type", "application/json")
	directRec := httptest.NewRecorder()
	latchedState.handleSetupComplete(directRec, directReq)
	if directRec.Code != http.StatusConflict {
		t.Fatalf("handler-level setup complete on provisioned instance: %d %s, want 409",
			directRec.Code, directRec.Body.String())
	}
}

// #1100: the marker survives restart — a brand-new state on the same state
// dir latches immediately even with zero users in state.json.
func TestProvisionedLatchSurvivesRestartWithZeroUsers(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	provisioned := &managementState{
		statePath: statePath,
		users:     []User{{Username: "admin", Role: "admin"}},
		sessions:  mustNewSessionRegistry(""),
	}
	provisioned.mu.Lock()
	provisioned.noteUsersProvisionedLocked()
	provisioned.mu.Unlock()
	marker := filepath.Join(dir, usersProvisionedMarkerName)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker not written: %v", err)
	}

	// "Restart" with a zero-user state (e.g. a restored pre-setup backup):
	// the persisted marker must be re-adopted so dev-anonymous stays closed.
	restarted := &managementState{
		statePath:         statePath,
		allowDevAnonymous: true,
		sessions:          mustNewSessionRegistry(""),
	}
	var invoked bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { invoked = true })
	handler := authMiddlewareWithOptions(restarted, authMiddlewareOptions{AllowDevAnonymous: true}, next)
	req := httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || invoked {
		t.Fatalf("provisioned-then-emptied instance granted access: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "veil admin") {
		t.Fatalf("401 missing recovery hint: %s", rec.Body.String())
	}
}

// #1112: the NaivePassword/admin fallback login must produce a session that
// actually survives the next request. The bootstrap marker exempts it from
// user-match revocation ONLY while the fallback precondition holds.
func TestNaivePasswordFallbackSessionUsableAndExpires(t *testing.T) {
	registry := mustNewSessionRegistry("")
	state := &managementState{
		sessions: registry,
		settings: Settings{NaivePassword: "the-dev-password"},
	}
	var echo http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		username, _ := r.Context().Value(contextKeyUsername).(string)
		role, _ := r.Context().Value(contextKeyRole).(string)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("user=" + username + " role=" + role))
	}
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{AllowDevAnonymous: false, Token: "tok"}, echo)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"admin","password":"the-dev-password"}`))
	loginRec := httptest.NewRecorder()
	state.handleLoginWithRevalidation(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("fallback login: %d %s", loginRec.Code, loginRec.Body.String())
	}
	var loginBody struct {
		CSRFToken string `json:"csrfToken"`
		Role      string `json:"role"`
	}
	if err := json.Unmarshal(loginRec.Body.Bytes(), &loginBody); err != nil || loginBody.Role != "admin" {
		t.Fatalf("login body: %v %s", err, loginRec.Body.String())
	}
	var token string
	for _, cookie := range loginRec.Result().Cookies() {
		if cookie.Name == "veil_session" {
			token = cookie.Value
		}
	}
	if token == "" {
		t.Fatal("no session cookie")
	}
	// The session is marked bootstrap in the registry.
	sess, ok := registry.Get(token)
	if !ok || !sess.Bootstrap {
		t.Fatalf("fallback session missing bootstrap marker: %+v ok=%v", sess, ok)
	}
	// The middleware must NOT revoke it on the next request (the #1112 bug).
	req := httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "user=admin role=admin") {
		t.Fatalf("bootstrap session rejected on next request: %d %s", rec.Code, rec.Body.String())
	}

	// Once a real account appears the bootstrap session is revoked outright —
	// even when it shares the new account's username.
	state.mu.Lock()
	state.users = []User{{Username: "admin", Role: "admin", PasswordHash: "x"}}
	state.noteUsersProvisionedLocked()
	state.mu.Unlock()
	if _, ok := registry.Get(token); ok {
		t.Fatal("bootstrap session survived provisioning")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: token})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bootstrap session honored after provisioning: %d", rec.Code)
	}
}

// #1112: ordinary sessions still revoke when their user disappears — the
// bootstrap exemption must not leak into normal sessions.
func TestOrdinarySessionStillRevokesWhenUserDisappears(t *testing.T) {
	registry := mustNewSessionRegistry("")
	state := &managementState{
		sessions: registry,
		users:    []User{{Username: "alice", Role: "viewer", PasswordHash: "x"}},
	}
	session, err := registry.Create(SessionCreateInput{Username: "alice", Role: "viewer"})
	if err != nil {
		t.Fatal(err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{Token: "tok"}, next)

	state.mu.Lock()
	state.users = nil
	state.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	req.AddCookie(&http.Cookie{Name: "veil_session", Value: session.Token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ordinary session honored after its user vanished: %d", rec.Code)
	}
	if _, ok := registry.Get(session.Token); ok {
		t.Fatal("orphaned ordinary session was not deleted")
	}
}

// #1090: a durable idempotency WAITER must re-check the fingerprint after
// waiting. When the reservation it waits on is replaced by another request's
// (different fingerprint), the waiter gets 409 — never a replay of the other
// request's response.
func TestDurableIdempotencyWaiterRejectsFingerprintSwap(t *testing.T) {
	db := openApplyTestDB(t)
	defer db.Close()
	store := newIdempotencyStore(db)
	defer store.Close()

	releaseOwner := make(chan struct{})
	var ownerStarted atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ownerStarted.Add(1)
		<-releaseOwner
		writeJSONStatus(w, http.StatusCreated, map[string]any{"owner": true})
	}))
	freeHandler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONStatus(w, http.StatusCreated, map[string]any{"other": true})
	}))

	request := func(body, key string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/test/wait-swap", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		return req
	}

	// Owner reserves scope K with body A and blocks inside the handler.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.ServeHTTP(httptest.NewRecorder(), request(`{"a":1}`, "swap-key"))
	}()
	for ownerStarted.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	// Waiter sends the same key + same body: it enters waitDurable.
	waiterDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, request(`{"a":1}`, "swap-key"))
		waiterDone <- rec
	}()
	// Let the waiter enter waitDurable (first poll is on the notification).
	time.Sleep(150 * time.Millisecond)

	// The owner's reservation disappears (crash/abort semantics) and a
	// DIFFERENT request (same key, different body) claims and completes the
	// scope while the waiter is asleep. Both the record row and its domain
	// operation tracking row go — abortDurable abandons the operation and
	// the next reservation's cleanup removes it.
	if _, err := db.Exec(`DELETE FROM idempotency_records`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM domain_operations`); err != nil {
		t.Fatal(err)
	}
	otherRec := httptest.NewRecorder()
	freeHandler.ServeHTTP(otherRec, request(`{"different":2}`, "swap-key"))
	if otherRec.Code != http.StatusCreated {
		t.Fatalf("replacement request: %d %s", otherRec.Code, otherRec.Body.String())
	}

	select {
	case rec := <-waiterDone:
		if rec.Code != http.StatusConflict {
			t.Fatalf("waiter replayed another request's response: %d %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), `"other":true`) || rec.Header().Get("Idempotency-Replayed") == "true" {
			t.Fatalf("waiter replayed foreign response body: %d %s", rec.Code, rec.Body.String())
		}
	case <-time.After(10 * time.Second):
		close(releaseOwner)
		t.Fatal("waiter never returned after fingerprint swap")
	}
	close(releaseOwner)
	wg.Wait()
}

// #1090: the non-durable in-memory path must behave identically — a waiter
// whose scope is reused by a different fingerprint gets 409, not a replay.
func TestInMemoryIdempotencyWaiterRejectsFingerprintSwap(t *testing.T) {
	store := newIdempotencyStore()
	defer store.Close()

	releaseOwner := make(chan struct{})
	var ownerStarted atomic.Int32
	handler := store.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ownerStarted.Add(1)
		<-releaseOwner
		writeJSONStatus(w, http.StatusCreated, map[string]any{"owner": true})
	}))
	request := func(body string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/test/mem-swap", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "mem-key")
		return req
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		handler.ServeHTTP(httptest.NewRecorder(), request(`{"a":1}`))
	}()
	for ownerStarted.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	waiterDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, request(`{"a":1}`))
		waiterDone <- rec
	}()
	time.Sleep(150 * time.Millisecond)

	// Free the owner so its entry completes, then a different-fingerprint
	// request conflicts immediately (in-memory entries are never replaced
	// mid-wait — the release path only produces the legitimate replay).
	close(releaseOwner)
	wg.Wait()
	select {
	case rec := <-waiterDone:
		// The in-memory path DOES replay the owner's identical-fingerprint
		// response — same key, same request — which is correct.
		if rec.Code != http.StatusCreated {
			t.Fatalf("in-memory waiter: %d %s", rec.Code, rec.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-memory waiter never completed")
	}
}

// #1088: the speedtest handler must not serialize raw CLI output for
// viewers — the result struct no longer carries it.
func TestSpeedtestResponseOmitsRawCLIJSON(t *testing.T) {
	original := speedtestRunner
	speedtestRunner = func(_ *http.Request) (SpeedtestResult, error) {
		return SpeedtestResult{Server: "sponsor - name", PingMS: 12.5, DownloadMbps: 100, UploadMbps: 40}, nil
	}
	t.Cleanup(func() { speedtestRunner = original })

	req := httptest.NewRequest(http.MethodPost, "/api/tools/speedtest", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	DiagnosticToolRoutes{}.handleSpeedtest(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("speedtest: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, exists := body["raw"]; exists {
		t.Fatalf("viewer speedtest response leaked raw CLI JSON: %s", rec.Body.String())
	}
	for _, field := range []string{"server", "pingMs", "downloadMbps", "uploadMbps"} {
		if _, exists := body[field]; !exists {
			t.Fatalf("speedtest response missing structured field %s: %s", field, rec.Body.String())
		}
	}
}

// #1101: IPv6 addresses inside one /64 share login throttle/backoff state;
// a different /64 gets a fresh budget. This is the rotation the issue
// reported: one delegated prefix cannot mint unlimited attempts.
func TestLoginThrottleKeyAggregatesIPv6Slash64(t *testing.T) {
	reqA := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	reqA.RemoteAddr = "[2001:db8:aaaa:bbbb::1]:443"
	reqB := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	reqB.RemoteAddr = "[2001:db8:aaaa:bbbb::99]:443"
	reqC := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	reqC.RemoteAddr = "[2001:db8:aaaa:cccc::1]:443"
	reqV4 := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	reqV4.RemoteAddr = "203.0.113.7:443"

	if loginThrottleKey(reqA, "admin") != loginThrottleKey(reqB, "admin") {
		t.Fatal("two addresses inside one /64 must share the login throttle key")
	}
	if loginThrottleKey(reqA, "admin") == loginThrottleKey(reqC, "admin") {
		t.Fatal("addresses in different /64s must not share a throttle bucket")
	}
	if !strings.HasPrefix(loginThrottleKey(reqA, "admin"), "2001:db8:aaaa:bbbb::/64|") {
		t.Fatalf("throttle key not normalized to /64: %q", loginThrottleKey(reqA, "admin"))
	}
	if !strings.HasPrefix(loginThrottleKey(reqV4, "admin"), "203.0.113.7|") {
		t.Fatalf("IPv4 throttle key changed: %q", loginThrottleKey(reqV4, "admin"))
	}
}

// #1101: the global bcrypt work ceiling refuses additional concurrent hash
// work rather than letting a spray pin every CPU.
func TestBcryptWorkCeilingSaturates(t *testing.T) {
	held := make([]func(), 0, cap(bcryptWorkSlots))
	for i := 0; i < cap(bcryptWorkSlots); i++ {
		release := acquireBcryptWork()
		if release == nil {
			t.Fatalf("slot %d unexpectedly refused", i)
		}
		held = append(held, release)
	}
	if release := acquireBcryptWork(); release != nil {
		t.Fatal("bcrypt work slots did not saturate")
	}
	for _, release := range held {
		release()
	}
}

// #1100: dev-anonymous on a never-provisioned instance still works — the
// latch only closes AFTER first provisioning.
func TestDevAnonymousStillWorksBeforeProvisioning(t *testing.T) {
	state := &managementState{sessions: mustNewSessionRegistry("")}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, _ := r.Context().Value(contextKeyUsername).(string)
		_, _ = w.Write([]byte(username))
	})
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{AllowDevAnonymous: true}, next)
	req := httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "dev-anonymous") {
		t.Fatalf("fresh instance dev-anonymous broken: %d %s", rec.Code, rec.Body.String())
	}
}

// #1100: deleting every user in-process also latches — a zero-user state
// after provisioning is a lockdown, not a fresh install.
func TestInMemoryProvisionedLatchClosesAnonymousAfterUserWipe(t *testing.T) {
	dir := t.TempDir()
	state := &managementState{
		statePath:         filepath.Join(dir, "state.json"),
		allowDevAnonymous: true,
		users:             []User{{Username: "admin", Role: "admin"}},
		sessions:          mustNewSessionRegistry(""),
	}
	state.mu.Lock()
	state.noteUsersProvisionedLocked()
	state.users = nil // simulate admin wiping all users / zero-user restore
	latch := state.usersProvisionedLocked()
	state.mu.Unlock()
	if !latch {
		t.Fatal("latch lost when users dropped to zero")
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := authMiddlewareWithOptions(state, authMiddlewareOptions{AllowDevAnonymous: true}, next)
	req := httptest.NewRequest(http.MethodGet, "/api/inbounds", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-wipe anonymous access: %d, want 401", rec.Code)
	}
}

var _ = sql.ErrNoRows // keep the database/sql import honest across build tags
