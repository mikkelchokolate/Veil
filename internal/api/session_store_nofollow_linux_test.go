//go:build linux

package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// #1284: the session directory (/var/lib/veil) is writable by the veil
// service account, and root CLI paths (`veil admin set` →
// revokePersistedUserSessions → DeleteByUsername) append to its journal. A
// leaf planted by that account must never redirect a root write or read.

// seedSessionStore writes a real snapshot holding one "alice" session, the
// precondition that makes DeleteByUsername reach the journal append.
func seedSessionStore(t *testing.T) (path string, token string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "sessions.json")
	panel, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := panel.Create(SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("seed snapshot missing or irregular: %v %v", info, err)
	}
	return path, session.Token
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("%s was created through the planted symlink (lstat err=%v)", path, err)
	}
}

// The full root-side chain from the issue: a dangling journal symlink planted
// before the CLI opens the store must not become a root create of the target.
func TestSessionJournalDanglingSymlinkIsNotFollowedByRevocation(t *testing.T) {
	path, token := seedSessionStore(t)
	target := filepath.Join(t.TempDir(), "sudoers.d-planted")
	journal := path + ".journal"
	if err := os.Symlink(target, journal); err != nil {
		t.Fatal(err)
	}

	cli, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatalf("open store with planted journal symlink: %v", err)
	}
	if removed, err := cli.DeleteByUsername("alice"); err != nil || removed != 1 {
		t.Fatalf("DeleteByUsername = %d, %v; want 1, nil", removed, err)
	}
	assertNotExist(t, target)

	// The planted link was disarmed (quarantined) and replaced by a real
	// journal that carries the revocation across a restart.
	info, err := os.Lstat(journal)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("journal mode = %v, want a regular file", info.Mode())
	}
	reopened, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reopened.Get(token); ok {
		t.Fatal("revoked session survived restart")
	}
}

// A symlink swapped in after load (between load and append) must fail the
// append closed instead of writing into an existing file the attacker chose.
func TestSessionJournalAppendRefusesSymlinkSwappedAfterLoad(t *testing.T) {
	path, _ := seedSessionStore(t)
	cli, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "existing-empty")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path + ".journal")
	if err := os.Symlink(target, path+".journal"); err != nil {
		t.Fatal(err)
	}

	if _, err := cli.DeleteByUsername("alice"); err == nil {
		t.Fatal("DeleteByUsername appended through a planted journal symlink")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("symlink target grew to %d bytes: root append followed the link", info.Size())
	}

	missing := filepath.Join(t.TempDir(), "never-created")
	if err := os.Remove(path + ".journal"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(missing, path+".journal"); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.DeleteByUsername("alice"); err == nil {
		t.Fatal("DeleteByUsername appended through a dangling journal symlink")
	}
	assertNotExist(t, missing)
}

// The snapshot read must not follow a planted symlink either: a root reader
// would otherwise load sessions (or arbitrary bytes) from another file.
func TestSessionSnapshotSymlinkIsRefused(t *testing.T) {
	source, _ := seedSessionStore(t)
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.Symlink(source, path); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSessionRegistry(path); err == nil {
		t.Fatal("NewSessionRegistry loaded a snapshot through a planted symlink")
	}

	registry, err := NewSessionRegistry(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere.json"), source); err != nil {
		t.Fatal(err)
	}
	if err := registry.Healthy(); err == nil {
		t.Fatal("Healthy() vouched for a snapshot path that is a symlink")
	}
}

// runWithin fails the test when fn has not returned within the deadline —
// a FIFO open/read without O_NONBLOCK parks forever. unblock releases a
// parked call so the goroutine does not outlive the test.
func runWithin(t *testing.T, what string, unblock func(), fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		unblock()
		t.Fatalf("%s blocked on a planted FIFO", what)
		return nil
	}
}

func releaseFIFO(path string) func() {
	return func() {
		// Open both ends non-blockingly so a parked reader or writer wakes.
		if w, err := os.OpenFile(path, os.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
			_ = w.Close()
		}
		if r, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK, 0); err == nil {
			_ = r.Close()
		}
	}
}

func TestSessionJournalFIFODoesNotHangAppendOrLoad(t *testing.T) {
	path, token := seedSessionStore(t)
	journal := path + ".journal"
	cli, err := NewSessionRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(journal)
	if err := unix.Mkfifo(journal, 0o600); err != nil {
		t.Fatal(err)
	}
	err = runWithin(t, "journal append", releaseFIFO(journal), func() error {
		_, err := cli.DeleteByUsername("alice")
		return err
	})
	if err == nil {
		t.Fatal("DeleteByUsername reported success writing into a FIFO")
	}

	// Loading with the FIFO still in place must not block either: the
	// non-regular journal is quarantined and the snapshot stays authoritative.
	var reopened *SessionRegistry
	err = runWithin(t, "journal load", releaseFIFO(journal), func() error {
		var err error
		reopened, err = NewSessionRegistry(path)
		return err
	})
	if err != nil {
		t.Fatalf("load with FIFO journal: %v", err)
	}
	if _, ok := reopened.Get(token); !ok {
		t.Fatal("snapshot session lost after quarantining a FIFO journal")
	}
	if info, err := os.Lstat(journal); err == nil && info.Mode()&os.ModeNamedPipe != 0 {
		t.Fatal("FIFO journal left in place after load")
	}
}
