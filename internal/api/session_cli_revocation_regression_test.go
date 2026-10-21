package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Regression tests for #1265: `veil admin set/reset/rotate-key` revoke Panel
// sessions from a second, short-lived process. The running Panel must pick
// the revocation up on SIGHUP, and a session write the Panel makes before it
// handles the signal must not undo the revocation.

func cliRevocationFixture(t *testing.T) (statePath string, panel *SessionRegistry, base time.Time) {
	t.Helper()
	dir := t.TempDir()
	statePath = filepath.Join(dir, "state.json")
	panel, err := NewSessionRegistry(persistedSessionStorePath(statePath))
	if err != nil {
		t.Fatal(err)
	}
	// Sessions are minted a minute in the past so the CLI's wall-clock
	// Through bound is strictly after them; later logins are stamped in the
	// future so they are strictly after it.
	base = time.Now().UTC().Add(-time.Minute)
	panel.now = func() time.Time { return base }
	return statePath, panel, base
}

func mustCreate(t *testing.T, registry *SessionRegistry, username, role string) Session {
	t.Helper()
	session, err := registry.Create(SessionCreateInput{Username: username, Role: role})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func TestPanelReloadDropsSessionsTheCLIRevokedForOneUser(t *testing.T) {
	statePath, panel, _ := cliRevocationFixture(t)
	alice := mustCreate(t, panel, "alice", "admin")
	bob := mustCreate(t, panel, "bob", "viewer")

	if err := RevokeUsernamePersistedSessions(statePath, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := panel.ReloadFromDisk(); err != nil {
		t.Fatal(err)
	}
	if _, ok := panel.Get(alice.Token); ok {
		t.Fatal("running Panel kept a session the CLI revoked")
	}
	if got, ok := panel.Get(bob.Token); !ok {
		t.Fatal("reload dropped an unrelated user's session")
	} else if got.CSRFToken != bob.CSRFToken {
		t.Fatal("reload lost the surviving session's raw CSRF token")
	}
	if !panel.ValidateCSRF(bob.Token, bob.CSRFToken) {
		t.Fatal("reload lost the surviving session's CSRF token")
	}
}

func TestCLIUserRevocationSurvivesPanelUpsertBeforeReload(t *testing.T) {
	statePath, panel, base := cliRevocationFixture(t)
	alice := mustCreate(t, panel, "alice", "admin")

	if err := RevokeUsernamePersistedSessions(statePath, "alice"); err != nil {
		t.Fatal(err)
	}
	// Before the Panel handles SIGHUP it still holds alice's session in
	// memory and journals an upsert for it (here: a second-factor mark; a
	// LastSeen refresh takes the same path).
	if ok, err := panel.MarkSecondFactorPersisted(alice.Token); err != nil || !ok {
		t.Fatalf("upsert before reload: ok=%v err=%v", ok, err)
	}
	// A legitimate login after the CLI ran must survive.
	panel.now = func() time.Time { return base.Add(2 * time.Minute) }
	fresh := mustCreate(t, panel, "alice", "admin")

	if err := panel.ReloadFromDisk(); err != nil {
		t.Fatal(err)
	}
	if _, ok := panel.Get(alice.Token); ok {
		t.Fatal("a Panel upsert before SIGHUP resurrected the revoked session")
	}
	if _, ok := panel.Get(fresh.Token); !ok {
		t.Fatal("the revocation intent also revoked a login made after it")
	}
	restarted, err := NewSessionRegistry(persistedSessionStorePath(statePath))
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = panel.now
	if _, ok := restarted.Get(alice.Token); ok {
		t.Fatal("revoked session came back after a restart")
	}
	if _, ok := restarted.Get(fresh.Token); !ok {
		t.Fatal("post-revocation login lost after a restart")
	}
}

func TestCLIRevokeAllIsNotUndoneByThePanelRewritingSessions(t *testing.T) {
	statePath, panel, base := cliRevocationFixture(t)
	alice := mustCreate(t, panel, "alice", "admin")
	bob := mustCreate(t, panel, "bob", "viewer")

	if err := RevokeAllPersistedSessions(statePath); err != nil {
		t.Fatal(err)
	}
	// The old implementation deleted sessions.json; the Panel's next write
	// then saw "no snapshot" and rewrote every in-memory session to disk.
	if _, err := os.Lstat(persistedSessionStorePath(statePath)); err != nil {
		t.Fatalf("revoke-all must leave a session snapshot in place: %v", err)
	}
	// Panel writes before handling SIGHUP: an upsert of an old session and a
	// brand-new login.
	if ok, err := panel.MarkSecondFactorPersisted(bob.Token); err != nil || !ok {
		t.Fatalf("upsert before reload: ok=%v err=%v", ok, err)
	}
	panel.now = func() time.Time { return base.Add(2 * time.Minute) }
	fresh := mustCreate(t, panel, "carol", "admin")

	// Without the reload the Panel keeps serving the old sessions; this is
	// what SIGHUP has to fix.
	if _, ok := panel.Get(alice.Token); !ok {
		t.Fatal("fixture: expected the in-memory session before reload")
	}
	if err := panel.ReloadFromDisk(); err != nil {
		t.Fatal(err)
	}
	for name, token := range map[string]string{"alice": alice.Token, "bob": bob.Token} {
		if _, ok := panel.Get(token); ok {
			t.Fatalf("%s's pre-rotation session survived revoke-all", name)
		}
	}
	if _, ok := panel.Get(fresh.Token); !ok {
		t.Fatal("revoke-all also revoked a login made after it")
	}
	restarted, err := NewSessionRegistry(persistedSessionStorePath(statePath))
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = panel.now
	if _, ok := restarted.Get(bob.Token); ok {
		t.Fatal("revoke-all was undone across a restart")
	}
	if _, ok := restarted.Get(fresh.Token); !ok {
		t.Fatal("post-revocation login lost after a restart")
	}
}

func TestCLIRevokeAllSurvivesPanelCompaction(t *testing.T) {
	statePath, panel, base := cliRevocationFixture(t)
	alice := mustCreate(t, panel, "alice", "admin")
	if err := RevokeAllPersistedSessions(statePath); err != nil {
		t.Fatal(err)
	}
	if err := panel.ReloadFromDisk(); err != nil {
		t.Fatal(err)
	}
	panel.now = func() time.Time { return base.Add(2 * time.Minute) }
	fresh := mustCreate(t, panel, "alice", "admin")
	panel.mu.Lock()
	err := panel.compactJournalLocked()
	panel.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewSessionRegistry(persistedSessionStorePath(statePath))
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = panel.now
	if _, ok := restarted.Get(alice.Token); ok {
		t.Fatal("revoked session came back after a compaction")
	}
	if _, ok := restarted.Get(fresh.Token); !ok {
		t.Fatal("compaction lost a live session")
	}
}

func TestCLIRevocationSkipsMissingStoreDirectory(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "absent", "state.json")
	if err := RevokeAllPersistedSessions(statePath); err != nil {
		t.Fatal(err)
	}
	if err := RevokeUsernamePersistedSessions(statePath, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(statePath)); !os.IsNotExist(err) {
		t.Fatalf("CLI revocation created the store directory: %v", err)
	}
}

func TestRevocationCancelRetractsOnlyItsOwnIntent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-time.Minute)
	registry.now = func() time.Time { return base }
	alice := mustCreate(t, registry, "alice", "admin")

	registry.now = func() time.Time { return base.Add(time.Second) }
	panelIntent, err := registry.MarkUsernameRevocationPending("alice")
	if err != nil {
		t.Fatal(err)
	}
	// A second intent for the same user, as the root CLI journals it while
	// the Panel's own operation is still in flight.
	registry.now = func() time.Time { return base.Add(2 * time.Second) }
	if _, err := registry.MarkUsernameRevocationPending("alice"); err != nil {
		t.Fatal(err)
	}
	if err := registry.CancelUsernameRevocation(panelIntent); err != nil {
		t.Fatal(err)
	}
	if len(registry.pendingRevocations) != 1 {
		t.Fatalf("in-memory cancel retracted %d intents, want only its own", 2-len(registry.pendingRevocations))
	}
	reloaded, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(alice.Token); ok {
		t.Fatal("cancelling one intent retracted another intent for the same user")
	}
}

func TestLegacyRevocationCancelWithoutBoundStillRetractsAll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	registry, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	alice := mustCreate(t, registry, "alice", "admin")
	if _, err := registry.MarkUsernameRevocationPending("alice"); err != nil {
		t.Fatal(err)
	}
	// Journal written by a pre-#1265 binary: the cancel carries no bound.
	registry.mu.Lock()
	err = registry.appendJournalLocked(sessionJournalRecord{Operation: "revoke_cancel", Username: "alice"})
	registry.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(alice.Token); !ok {
		t.Fatal("legacy unbounded cancel no longer retracts the user's intents")
	}
}

func TestRevokeAllJournalRecordRejectsUsername(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	body := `{"operation":"revoke_all","username":"alice","through":"2026-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(path+".journal", []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSessionRegistry(path); err == nil || !strings.Contains(err.Error(), "revocation intent") {
		t.Fatalf("malformed revoke_all accepted: %v", err)
	}
	body = `{"operation":"revoke_all"}` + "\n"
	if err := os.WriteFile(path+".journal", []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSessionRegistry(path); err == nil {
		t.Fatal("revoke_all without a bound accepted")
	}
}

func TestManagementStateReloadPicksUpCLISessionRevocation(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	state := newManagementState(ServerInfo{StatePath: statePath, KeyPath: filepath.Join(dir, "state.key"), Mode: "dev"})
	defer closeClientSubsystem(state)
	if err := NewManagementStateLifecycle(state).SaveLocked(); err != nil {
		t.Fatal(err)
	}
	alice := mustCreate(t, state.sessionRegistry(), "alice", "admin")
	bob := mustCreate(t, state.sessionRegistry(), "bob", "viewer")
	if err := RevokeUsernamePersistedSessions(statePath, "alice"); err != nil {
		t.Fatal(err)
	}
	_ = state.Reload()
	if _, ok := state.sessionRegistry().Get(alice.Token); ok {
		t.Fatal("SIGHUP reload did not apply the CLI session revocation")
	}
	if _, ok := state.sessionRegistry().Get(bob.Token); !ok {
		t.Fatal("SIGHUP reload dropped an unrelated session")
	}
}
