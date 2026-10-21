package api

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Cross-process session revocation (#1265).
//
// The root CLI (`veil admin set` / `reset` / `rotate-key`) cannot reach the
// running Panel's in-memory SessionRegistry. It therefore journals durable,
// positional revocation intents next to state.json and signals the Panel,
// whose SIGHUP reload calls ReloadFromDisk so the on-disk store, intents
// included, becomes authoritative for the running process too.
//
// The intents are positional (they cover sessions created at or before their
// Through bound) and are re-applied after every journal replay, so an upsert
// the Panel appends for an old session between the CLI write and the SIGHUP
// (a LastSeen refresh, say) cannot resurrect it. A legitimate login made
// after the CLI ran is newer than Through and survives.

// RevokeUsernamePersistedSessions journals a revoke_username intent for
// username and deletes the user's sessions from the on-disk store. It never
// cancels the intent: the credential change it pairs with has already been
// committed by the caller.
func RevokeUsernamePersistedSessions(statePath, username string) error {
	sessionPath := persistedSessionStorePath(statePath)
	if sessionPath == "" || username == "" {
		return nil
	}
	if missing, err := sessionStoreDirMissing(sessionPath); err != nil || missing {
		return err
	}
	registry, err := NewSessionRegistry(sessionPath)
	if err != nil {
		return fmt.Errorf("load Panel sessions: %w", err)
	}
	if _, err := registry.MarkUsernameRevocationPending(username); err != nil {
		return fmt.Errorf("journal session revocation for %q: %w", username, err)
	}
	if _, err := registry.DeleteByUsername(username); err != nil {
		return fmt.Errorf("revoke sessions for %q: %w", username, err)
	}
	return nil
}

// RevokeAllPersistedSessions revokes every session in the on-disk store
// without relying on the store being loadable: it publishes an empty snapshot
// (dropping the old journal) and then journals a revoke_all intent.
//
// Earlier versions deleted the snapshot and journal instead. The running
// Panel then treated the missing snapshot as "never saved" on its next
// session write and rewrote every in-memory session to disk, undoing the
// invalidation permanently. With an empty snapshot in place the Panel keeps
// appending to the journal, and the revoke_all intent is replayed after any
// such append.
func RevokeAllPersistedSessions(statePath string) error {
	sessionPath := persistedSessionStorePath(statePath)
	if sessionPath == "" {
		return nil
	}
	if missing, err := sessionStoreDirMissing(sessionPath); err != nil || missing {
		return err
	}
	registry := newSessionRegistryWithoutLoad(sessionPath)
	registry.mu.Lock()
	defer registry.mu.Unlock()
	intent := sessionRevocationIntent{Through: registry.now().UTC(), All: true}
	// Track the intent in memory first so a compaction triggered by the
	// append (oversized journal) still carries it in its checkpoint.
	registry.pendingRevocations = append(registry.pendingRevocations, intent)
	if err := registry.saveLocked(); err != nil {
		return fmt.Errorf("reset Panel session snapshot: %w", err)
	}
	if err := registry.appendJournalLocked(sessionJournalRecord{Operation: "revoke_all", Through: intent.Through}); err != nil {
		return fmt.Errorf("journal session revocation: %w", err)
	}
	return nil
}

// ReloadFromDisk makes the on-disk snapshot and journal authoritative for the
// running registry: sessions another process deleted or revoked disappear
// from memory. The Panel calls it on SIGHUP (#1265). Every in-process session
// write is persisted synchronously, so the only sessions it drops are ones
// removed on disk by someone else (or ones whose persistence already failed,
// which fails closed).
//
// On a load error the in-memory sessions are left untouched and the error is
// returned for the caller to log.
func (r *SessionRegistry) ReloadFromDisk() error {
	if r == nil || r.path == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	fresh := newSessionRegistryWithoutLoad(r.path)
	fresh.now = r.now
	fresh.idleTimeout = r.idleTimeout
	fresh.absoluteTimeout = r.absoluteTimeout
	fresh.persistInterval = r.persistInterval
	// Loading under r.mu keeps this process's own journal appends from
	// interleaving with the read.
	if err := fresh.load(); err != nil {
		return fmt.Errorf("reload Panel sessions from %s: %w", r.path, err)
	}
	rawCSRF := make(map[string]string, len(fresh.sessions))
	for hash := range fresh.sessions {
		if csrf, ok := r.rawCSRF[hash]; ok {
			rawCSRF[hash] = csrf
		}
	}
	r.sessions = fresh.sessions
	r.rawCSRF = rawCSRF
	// r.pendingRevocations is kept: those are this process's own outstanding
	// intents, still awaiting their delete_many or cancel. Replay has just
	// applied them, which only revokes sessions they were going to revoke.
	return nil
}

// sessionStoreDirMissing reports whether the directory that would hold the
// session store does not exist. No Panel has ever run there, so there is
// nothing to revoke, and the root CLI must not create a root-owned directory
// in its place.
func sessionStoreDirMissing(sessionPath string) (bool, error) {
	info, err := os.Stat(filepath.Dir(sessionPath))
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("session store parent %s is not a directory", filepath.Dir(sessionPath))
	}
	return false, nil
}
