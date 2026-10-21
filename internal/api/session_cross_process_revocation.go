package api

import (
	"bytes"
	"encoding/json"
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
//
// The whole load, intent append and delete runs under the cross-process
// store lock, so a Panel snapshot rewrite or compaction either absorbs the
// intent or runs entirely before it.
func RevokeUsernamePersistedSessions(statePath, username string) error {
	sessionPath := persistedSessionStorePath(statePath)
	if sessionPath == "" || username == "" {
		return nil
	}
	if missing, err := sessionStoreDirMissing(sessionPath); err != nil || missing {
		return err
	}
	registry := newUnloadedSessionRegistry(sessionPath)
	release, err := registry.holdStoreLock()
	if err != nil {
		return fmt.Errorf("lock Panel sessions: %w", err)
	}
	defer release()
	if err := registry.loadAndBound(); err != nil {
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
	// One store-lock hold across the empty snapshot and the intent append:
	// a Panel append can then not land between them and be discarded, and a
	// Panel rewrite either runs before the snapshot or absorbs the intent.
	release, err := registry.acquireStoreLockLocked()
	if err != nil {
		return fmt.Errorf("lock Panel sessions: %w", err)
	}
	defer release()
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
	// Read the snapshot and journal as one consistent pair: a rewrite in
	// another process cannot publish between the two reads.
	release, err := r.acquireStoreLockLocked()
	if err != nil {
		return fmt.Errorf("lock Panel sessions at %s: %w", r.path, err)
	}
	defer release()
	fresh := newSessionRegistryWithoutLoad(r.path)
	fresh.now = r.now
	fresh.idleTimeout = r.idleTimeout
	fresh.absoluteTimeout = r.absoluteTimeout
	fresh.persistInterval = r.persistInterval
	// Loading under r.mu keeps this process's own journal appends from
	// interleaving with the read.
	if err := fresh.loadFromDisk(false); err != nil {
		// Keep memory (the snapshot alone may still hold sessions only the
		// journal revoked), but still honour every revocation intent that
		// can be read from the journal.
		r.absorbDiskRevocationsLocked()
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
	// fresh.pendingRevocations (the CLI's intents) is dropped: replay has
	// already applied them to the map adopted above, so a later checkpoint
	// that no longer carries them cannot resurrect anything, and any rewrite
	// before that re-absorbs them from disk first.
	return nil
}

// absorbDiskRevocationsLocked applies the revocation intents in the on-disk
// journal to the in-memory sessions, exactly as a restart's replay would.
// Every whole-store rewrite (snapshot save, journal checkpoint) calls it
// first: the rewrite discards the journal, and another process (the root
// CLI) may have journaled a revocation since this process last read it
// (#1265). Intents this process itself still holds as pending are skipped,
// so an in-flight credential change keeps the session it means to keep.
//
// It is best effort: unreadable journals and malformed lines are skipped
// (the rewrite that follows replaces them anyway). Revoking is the only
// effect, so a stale intent can at worst force an extra re-login.
func (r *SessionRegistry) absorbDiskRevocationsLocked() {
	if r.path == "" || len(r.sessions) == 0 {
		return
	}
	body, err := readSessionStoreFile(r.journalPath())
	if err != nil {
		return
	}
	var intents []sessionRevocationIntent
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var record sessionJournalRecord
		if json.Unmarshal(line, &record) != nil {
			continue
		}
		switch record.Operation {
		case "revoke_username":
			intent := sessionRevocationIntent{Username: record.Username, Through: record.Through}
			if validRevocationIntent(intent) {
				intents = append(intents, intent)
			}
		case "revoke_all":
			intent := sessionRevocationIntent{Through: record.Through, All: true}
			if record.Username == "" && validRevocationIntent(intent) {
				intents = append(intents, intent)
			}
		case "revoke_cancel":
			if record.Username != "" {
				intents = cancelPendingRevocation(intents, record.Username, record.Through)
			}
		case "replace_all":
			intents = intents[:0]
			for _, intent := range record.Revocations {
				if validRevocationIntent(intent) {
					intents = append(intents, intent)
				}
			}
		}
	}
	for _, intent := range intents {
		if r.holdsPendingIntentLocked(intent) {
			continue
		}
		for tokenHash, session := range r.sessions {
			if intent.covers(session) {
				delete(r.sessions, tokenHash)
				delete(r.rawCSRF, tokenHash)
			}
		}
	}
}

// holdStoreLock takes the cross-process store lock for a caller that runs
// several registry operations as one unit (the root CLI); each operation
// re-enters it. Release it with the returned function.
func (r *SessionRegistry) holdStoreLock() (func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	release, err := r.acquireStoreLockLocked()
	if err != nil {
		return nil, err
	}
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		release()
	}, nil
}

func (r *SessionRegistry) holdsPendingIntentLocked(intent sessionRevocationIntent) bool {
	for _, pending := range r.pendingRevocations {
		if pending.All == intent.All && pending.Username == intent.Username && pending.Through.Equal(intent.Through) {
			return true
		}
	}
	return false
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
