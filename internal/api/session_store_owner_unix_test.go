//go:build unix

package api

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The root CLI writes the Panel's session store during revocation (#1265).
// Files it creates must be handed to the store directory's owner, or the
// unprivileged Panel can no longer read or append to them.
func TestRootCLISessionWritesMatchStoreDirectoryOwner(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	panel, err := NewSessionRegistry(persistedSessionStorePath(statePath))
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, panel, "alice", "admin")

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)

	origEUID, origChown := sessionStoreEUID, sessionStoreChown
	t.Cleanup(func() { sessionStoreEUID, sessionStoreChown = origEUID, origChown })
	type call struct {
		name     string
		uid, gid int
	}
	var calls []call
	sessionStoreChown = func(file *os.File, uid, gid int) error {
		calls = append(calls, call{filepath.Base(file.Name()), uid, gid})
		return nil
	}

	sessionStoreEUID = func() int { return 1000 }
	if err := RevokeAllPersistedSessions(statePath); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 {
		t.Fatalf("non-root writer chowned session files: %v", calls)
	}

	sessionStoreEUID = func() int { return 0 }
	if err := RevokeAllPersistedSessions(statePath); err != nil {
		t.Fatal(err)
	}
	if err := RevokeUsernamePersistedSessions(statePath, "alice"); err != nil {
		t.Fatal(err)
	}
	var snapshot, journal, lock int
	for _, c := range calls {
		if c.uid != int(stat.Uid) || c.gid != int(stat.Gid) {
			t.Fatalf("chown %s to %d:%d, want store directory owner %d:%d", c.name, c.uid, c.gid, stat.Uid, stat.Gid)
		}
		switch {
		case c.name == "sessions.json.journal":
			journal++
		case c.name == "sessions.json.lock":
			lock++
		case len(c.name) > 10 && c.name[:10] == ".sessions-":
			snapshot++
		}
	}
	if snapshot == 0 || journal == 0 || lock == 0 {
		t.Fatalf("root writes not handed to the store owner (snapshot=%d journal=%d lock=%d): %v", snapshot, journal, lock, calls)
	}
}

// The store lock is opened through the pinned directory like the journal: a
// planted symlink or an extra hard link at the lock path must be refused,
// not followed into (or chowned onto) another file.
func TestSessionStoreLockRefusesPlantedLinks(t *testing.T) {
	dir := t.TempDir()
	path := persistedSessionStorePath(filepath.Join(dir, "state.json"))
	target := filepath.Join(dir, "victim")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, sessionStoreLockPath(path)); err != nil {
		t.Fatal(err)
	}
	if file, err := lockSessionStore(path); err == nil {
		unlockSessionStore(file)
		t.Fatal("store lock followed a planted symlink")
	}
	if err := os.Remove(sessionStoreLockPath(path)); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, sessionStoreLockPath(path)); err != nil {
		t.Fatal(err)
	}
	if file, err := lockSessionStore(path); err == nil {
		unlockSessionStore(file)
		t.Fatal("store lock accepted a hard-linked inode")
	}
	registry := newUnloadedSessionRegistry(path)
	if _, err := registry.MarkUsernameRevocationPending("alice"); err == nil {
		t.Fatal("journal append ran without the store lock")
	}
}

// The lock must actually exclude another holder: a second open of the same
// store blocks until the first releases it.
func TestSessionStoreLockExcludesSecondHolder(t *testing.T) {
	path := persistedSessionStorePath(filepath.Join(t.TempDir(), "state.json"))
	first, err := lockSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan struct{})
	go func() {
		second, err := lockSessionStore(path)
		if err == nil {
			unlockSessionStore(second)
		}
		close(acquired)
	}()
	select {
	case <-acquired:
		t.Fatal("a second holder took the store lock while it was held")
	case <-time.After(100 * time.Millisecond):
	}
	unlockSessionStore(first)
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("the store lock was not handed over after release")
	}
}
