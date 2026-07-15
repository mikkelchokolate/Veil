package api

import "errors"

var errSessionRevocationPersistence = errors.New("failed to persist session revocation")

func cloneStoredSessionMap(source map[string]storedSession) map[string]storedSession {
	clone := make(map[string]storedSession, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func cloneSessionSecretMap(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func (r *SessionRegistry) mutateAndPersistSessions(mutate func() int) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	previousSessions := cloneStoredSessionMap(r.sessions)
	previousCSRF := cloneSessionSecretMap(r.rawCSRF)
	changed := mutate()
	if changed == 0 {
		return 0, nil
	}
	if err := r.saveLocked(); err != nil {
		r.sessions = previousSessions
		r.rawCSRF = previousCSRF
		return changed, err
	}
	return changed, nil
}

func (r *SessionRegistry) DeleteTokenPersisted(token string) (bool, error) {
	tokenHash := hashSessionSecret(token)
	changed, err := r.mutateAndPersistSessions(func() int {
		if _, ok := r.sessions[tokenHash]; !ok {
			return 0
		}
		delete(r.sessions, tokenHash)
		delete(r.rawCSRF, tokenHash)
		return 1
	})
	return changed > 0, err
}

func (r *SessionRegistry) DeleteByIDPersisted(id string) (bool, error) {
	changed, err := r.mutateAndPersistSessions(func() int {
		for tokenHash, session := range r.sessions {
			if session.ID != id {
				continue
			}
			delete(r.sessions, tokenHash)
			delete(r.rawCSRF, tokenHash)
			return 1
		}
		return 0
	})
	return changed > 0, err
}

func (r *SessionRegistry) DeleteUsernamePersisted(username string) (int, error) {
	return r.mutateAndPersistSessions(func() int {
		deleted := 0
		for tokenHash, session := range r.sessions {
			if session.Username != username {
				continue
			}
			delete(r.sessions, tokenHash)
			delete(r.rawCSRF, tokenHash)
			deleted++
		}
		return deleted
	})
}

func (r *SessionRegistry) DeleteAllExceptPersisted(currentToken string) (int, error) {
	currentHash := hashSessionSecret(currentToken)
	return r.mutateAndPersistSessions(func() int {
		deleted := 0
		for tokenHash := range r.sessions {
			if tokenHash == currentHash {
				continue
			}
			delete(r.sessions, tokenHash)
			delete(r.rawCSRF, tokenHash)
			deleted++
		}
		return deleted
	})
}
