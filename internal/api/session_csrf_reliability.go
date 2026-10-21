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
			// The status endpoint is the SPA's source of truth for "am I
			// logged in". Apply exactly the rules the API middleware applies
			// — user-match (a deleted user's session is revoked, #1266),
			// second-factor (#1172) and bootstrap-fallback validity — or a
			// cookie the middleware would revoke on the next protected call
			// reports authenticated (and gets a CSRF hash persisted) here.
			// The middleware's view of the role is the user row's current
			// role, not the one frozen into the session at login.
			username, role, ok := s.authorizeCookieSession(cookie.Value, sess)
			if !ok {
				writeJSON(w, map[string]any{"authenticated": false})
				return
			}
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
				"username":      username,
				"role":          role,
				"locale":        s.userLocale(username),
				"csrfToken":     csrf,
				// Lets factor-management UIs skip the redundant password
				// prompt when this session already satisfied a second
				// factor (#1171). Informational only — it never widens
				// authorization by itself.
				"secondFactor": sess.SecondFactor,
			})
			return
		}
	}

	writeJSON(w, map[string]any{"authenticated": false})
}
