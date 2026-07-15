package api

import "net/http"

func (r *SessionRegistry) EnsureCSRFPersisted(token string) (string, bool, error) {
	tokenHash := hashSessionSecret(token)
	r.mu.Lock()
	defer r.mu.Unlock()

	record, ok := r.sessions[tokenHash]
	if !ok || sessionExpired(record, r.now().UTC()) {
		return "", false, nil
	}
	if csrf := r.rawCSRF[tokenHash]; csrf != "" {
		return csrf, true, nil
	}
	csrf, err := generateRandomHex(32)
	if err != nil {
		return "", false, err
	}

	previousRecord := record
	previousCSRF, hadPreviousCSRF := r.rawCSRF[tokenHash]
	record.CSRFHash = hashSessionSecret(csrf)
	r.sessions[tokenHash] = record
	r.rawCSRF[tokenHash] = csrf
	if err := r.saveLocked(); err != nil {
		r.sessions[tokenHash] = previousRecord
		if hadPreviousCSRF {
			r.rawCSRF[tokenHash] = previousCSRF
		} else {
			delete(r.rawCSRF, tokenHash)
		}
		return "", false, err
	}
	return csrf, true, nil
}

func (s *managementState) handlePersistentAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	cookie, err := r.Cookie("veil_session")
	if err == nil {
		if sess, ok := s.sessionRegistry().Get(cookie.Value); ok {
			csrf, csrfOK, csrfErr := s.sessionRegistry().EnsureCSRFPersisted(cookie.Value)
			if csrfErr != nil {
				writeError(w, "failed to refresh session", http.StatusInternalServerError)
				return
			}
			if !csrfOK {
				writeJSON(w, map[string]any{"authenticated": false})
				return
			}
			writeJSON(w, map[string]any{
				"authenticated": true,
				"username":      sess.Username,
				"role":          sess.Role,
				"locale":        s.userLocale(sess.Username),
				"csrfToken":     csrf,
			})
			return
		}
	}

	writeJSON(w, map[string]any{"authenticated": false})
}
