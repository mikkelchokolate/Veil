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
