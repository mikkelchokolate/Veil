//go:build unix

package api

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
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
	var snapshot, journal int
	for _, c := range calls {
		if c.uid != int(stat.Uid) || c.gid != int(stat.Gid) {
			t.Fatalf("chown %s to %d:%d, want store directory owner %d:%d", c.name, c.uid, c.gid, stat.Uid, stat.Gid)
		}
		switch {
		case c.name == "sessions.json.journal":
			journal++
		case len(c.name) > 10 && c.name[:10] == ".sessions-":
			snapshot++
		}
	}
	if snapshot == 0 || journal == 0 {
		t.Fatalf("root writes not handed to the store owner (snapshot=%d journal=%d): %v", snapshot, journal, calls)
	}
}
