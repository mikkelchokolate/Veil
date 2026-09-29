package api

import "errors"

var errSessionRevocationPersistence = errors.New("failed to persist session revocation")

// mutateAndPersistSessions collects the token hashes selected by the caller and
// deletes them through a single delete_many journal record. Persisting each
// deletion separately would commit sessions 1..N-1 on disk before a failure on
// N forced a full in-memory restore, leaving sessions dead-on-disk but live in
// memory (#1060).
func (r *SessionRegistry) mutateAndPersistSessions(collect func() []string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	hashes := collect()
	if len(hashes) == 0 {
		return 0, nil
	}
	if err := r.deleteManyLocked(hashes); err != nil {
		// The count reports how many sessions were selected for deletion even
		// though the durable delete failed and the in-memory rows were
		// restored; callers rely on it to report the attempted revocation.
		return len(hashes), err
	}
	return len(hashes), nil
}

func (r *SessionRegistry) DeleteTokenPersisted(token string) (bool, error) {
	tokenHash := hashSessionSecret(token)
	changed, err := r.mutateAndPersistSessions(func() []string {
		if _, ok := r.sessions[tokenHash]; !ok {
			return nil
		}
		return []string{tokenHash}
	})
	return changed > 0, err
}

func (r *SessionRegistry) DeleteByIDPersisted(id string) (bool, error) {
	changed, err := r.mutateAndPersistSessions(func() []string {
		for tokenHash, session := range r.sessions {
			if session.ID != id {
				continue
			}
			return []string{tokenHash}
		}
		return nil
	})
	return changed > 0, err
}

func (r *SessionRegistry) DeleteUsernamePersisted(username string) (int, error) {
	changed, err := r.mutateAndPersistSessions(func() []string {
		var hashes []string
		for tokenHash, session := range r.sessions {
			if session.Username != username {
				continue
			}
			hashes = append(hashes, tokenHash)
		}
		return hashes
	})
	if err == nil {
		// Every session the pending revocation intents for this user could
		// still reach is now durably deleted, so the intents are fulfilled
		// and must not be carried into the next journal checkpoint (#1059).
		r.mu.Lock()
		r.pendingRevocations = dropPendingRevocations(r.pendingRevocations, username)
		r.mu.Unlock()
	}
	return changed, err
}

// DeleteBootstrapPersisted revokes every fallback-minted bootstrap session.
// Called when the first real user row appears (first-run setup, user create,
// CLI reset reload, snapshot restore): a session minted without knowing a
// real credential must never survive the creation of the account it mimics
// (#1112).
func (r *SessionRegistry) DeleteBootstrapPersisted() (int, error) {
	return r.mutateAndPersistSessions(func() []string {
		var hashes []string
		for tokenHash, session := range r.sessions {
			if !session.Bootstrap {
				continue
			}
			hashes = append(hashes, tokenHash)
		}
		return hashes
	})
}

// MarkSecondFactorPersisted records on the session that its owner's second
// factor was satisfied in this browser flow. It is journaled like any other
// upsert: a flag that exists only in memory would drop at restart and the
// middleware would revoke the still-valid session on the next request — a
// fail-closed but disruptive outcome the TOTP confirm path must not inflict
// on the operator who just proved the factor (#1172).
func (r *SessionRegistry) MarkSecondFactorPersisted(token string) (bool, error) {
	tokenHash := hashSessionSecret(token)
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.sessions[tokenHash]
	if !ok || sessionExpired(record, r.now().UTC()) {
		return false, nil
	}
	if record.SecondFactor {
		return true, nil
	}
	previous := record
	record.SecondFactor = true
	r.sessions[tokenHash] = record
	if err := r.persistUpsertLocked(record); err != nil {
		r.sessions[tokenHash] = previous
		return false, err
	}
	return true, nil
}

// DeleteUsernameExceptPersisted revokes every session owned by username
// except the caller's current token. TOTP enable/disable uses it so the
// session performing the factor change survives while every other login —
// including ones minted before the factor existed — is revoked (#1172).
//
// The caller pairs this with MarkUsernameRevocationPending + an explicit
// CancelUsernameRevocation: the kept session predates the intent's Through
// bound, so a replayed intent would delete it — only the journaled cancel
// keeps live and replayed state consistent (#1059 pattern).
func (r *SessionRegistry) DeleteUsernameExceptPersisted(username, keepToken string) (int, error) {
	keepHash := hashSessionSecret(keepToken)
	return r.mutateAndPersistSessions(func() []string {
		var hashes []string
		for tokenHash, session := range r.sessions {
			if session.Username != username || tokenHash == keepHash {
				continue
			}
			hashes = append(hashes, tokenHash)
		}
		return hashes
	})
}

func (r *SessionRegistry) DeleteAllExceptPersisted(currentToken string) (int, error) {
	currentHash := hashSessionSecret(currentToken)
	changed, err := r.mutateAndPersistSessions(func() []string {
		var hashes []string
		for tokenHash := range r.sessions {
			if tokenHash == currentHash {
				continue
			}
			hashes = append(hashes, tokenHash)
		}
		return hashes
	})
	if err == nil {
		// The surviving current session must still be alive after a journal
		// replay, so every pending revocation intent (which would delete it
		// at load when its username matches) is resolved here (#1059).
		r.mu.Lock()
		r.pendingRevocations = nil
		r.mu.Unlock()
	}
	return changed, err
}
