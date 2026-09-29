package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"golang.org/x/crypto/bcrypt"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// Self-service passkey management (#1171). Registration and deletion are
// credential-grade operations: the caller needs a session that already
// satisfies the second-factor requirement (SecondFactor mark, minted by a
// verified TOTP/passkey login or a factor enrollment confirmation) OR a fresh
// account password, so a stolen unlocked session cannot silently enroll or
// drop a credential. Challenges live in the in-memory ceremony store keyed
// by the session token; sign-count regressions are handled on the login path
// in auth_webauthn_verify.go.

// maxPasskeysPerUser bounds the per-account credential list: passkeys are
// stored in the management snapshot, which is written whole, so an
// unbounded list would be a state-size amplification vector.
const maxPasskeysPerUser = 16

// maxPasskeyNameLen caps the operator-supplied credential label — it is
// stored verbatim and rendered in the UI, so keep it short and plain.
const maxPasskeyNameLen = 64

var errPasskeyVerifyFailed = errors.New("passkey verification failed")

// errPasskeyLimit marks a per-account passkey cap hit discovered inside the
// register-finish mutation. Begin checks the cap pre-ceremony, but the
// re-check under the user lock is the authoritative one: two concurrent
// registrations on the same account must not exceed it (#1171 review).
var errPasskeyLimit = errors.New("passkey limit reached")

// passkeyListEntry is the only passkey shape the API ever emits: public
// metadata. The credential's public key, sign counter, and attestation
// details are server-side records and never leave the process.
type passkeyListEntry struct {
	ID         string   `json:"id"`
	Name       string   `json:"name,omitempty"`
	CreatedAt  string   `json:"createdAt,omitempty"`
	Transports []string `json:"transports,omitempty"`
	BackedUp   bool     `json:"backedUp,omitempty"`
}

func passkeyListEntryOf(passkey model.Passkey) passkeyListEntry {
	return passkeyListEntry{
		ID:         passkey.ID,
		Name:       passkey.Name,
		CreatedAt:  passkey.CreatedAt,
		Transports: append([]string(nil), passkey.Transports...),
		BackedUp:   passkey.BackupState,
	}
}

// decodeOptionalJSONRequest accepts either an empty body or a single JSON
// object so POST begin endpoints stay callable from plain clients.
func decodeOptionalJSONRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	return decodeJSONRequest(w, r, v)
}

// passkeyCredentialGate enforces the credential-grade precondition on every
// self-service passkey mutation: either the session already carries the
// second-factor mark, or the caller re-presents the account password. The
// password path is throttled through the login backoff family so the gate
// cannot be turned into a password oracle.
func (s *managementState) passkeyCredentialGate(w http.ResponseWriter, r *http.Request, session Session, user model.User, password string) bool {
	if session.SecondFactor {
		return true
	}
	if strings.TrimSpace(password) == "" {
		writeError(w, "password is required", http.StatusBadRequest)
		return false
	}
	throttleKey := "passkey-manage:" + loginThrottleKey(r, user.Username)
	if retryAfter := s.loginBackoffRemaining(throttleKey); retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeError(w, "too many attempts", http.StatusTooManyRequests)
		return false
	}
	releaseBcrypt := acquireBcryptWork()
	if releaseBcrypt == nil {
		w.Header().Set("Retry-After", "1")
		writeError(w, "too many attempts", http.StatusTooManyRequests)
		return false
	}
	matched := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil
	releaseBcrypt()
	if !matched {
		delay := s.recordLoginFailure(throttleKey)
		w.Header().Set("Retry-After", strconv.Itoa(int(delay.Seconds())+1))
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.passkey.manage", Target: user.Username,
			Success: false, Error: "invalid password",
		})
		writeError(w, "invalid password", http.StatusBadRequest)
		return false
	}
	return true
}

// handleMyPasskeys serves GET /api/v1/users/me/passkeys — the credential
// list for the session's own account.
func (s *managementState) handleMyPasskeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	_, user, ok := s.sessionUserForFactor(w, r)
	if !ok {
		return
	}
	entries := make([]passkeyListEntry, 0, len(user.Passkeys))
	for _, passkey := range user.Passkeys {
		entries = append(entries, passkeyListEntryOf(passkey))
	}
	writeJSON(w, map[string]any{"passkeys": entries})
}

// handleMyPasskeyRegisterBegin starts a WebAuthn registration ceremony for
// the session's own account. The challenge is kept server-side keyed by the
// session token; the response carries the PublicKeyCredentialCreationOptions
// the browser passes to navigator.credentials.create.
func (s *managementState) handleMyPasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	session, user, ok := s.sessionUserForFactor(w, r)
	if !ok {
		return
	}
	var req struct {
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if !decodeOptionalJSONRequest(w, r, &req) {
		return
	}
	if len(req.Name) > maxPasskeyNameLen {
		writeError(w, "name is too long", http.StatusBadRequest)
		return
	}
	if len(user.Passkeys) >= maxPasskeysPerUser {
		writeError(w, "passkey limit reached", http.StatusBadRequest)
		return
	}
	if !s.passkeyCredentialGate(w, r, session, user, req.Password) {
		return
	}
	wa, _, err := webAuthnForRequest(r)
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.passkey.register.begin", Target: user.Username,
			Success: false, Error: err.Error(),
		})
		writeError(w, "passkey registration is unavailable", http.StatusInternalServerError)
		return
	}
	waUser := webAuthnUser{username: user.Username, passkeys: user.Passkeys}
	// Exclusions keep the ceremony honest: an authenticator that already
	// holds a credential for this account must refuse rather than silently
	// mint a second one the server list would never reconcile.
	creation, sessionData, err := wa.BeginRegistration(waUser,
		webauthn.WithExclusions(passkeyDescriptors(user.Passkeys)),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationPreferred,
		}),
	)
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.passkey.register.begin", Target: user.Username,
			Success: false, Error: err.Error(),
		})
		writeError(w, "failed to start passkey registration", http.StatusInternalServerError)
		return
	}
	s.webAuthnChallenges().Put(currentSessionToken(r), user.Username, *sessionData)
	s.recordRequestAudit(r, audit.Record{
		Actor: user.Username, Action: "user.passkey.register.begin", Target: user.Username,
		Success: true,
	})
	writeJSON(w, creation)
}

// passkeyDescriptors renders the stored credentials as the library's
// exclusion/allowance descriptor list.
func passkeyDescriptors(passkeys []model.Passkey) []protocol.CredentialDescriptor {
	descriptors := make([]protocol.CredentialDescriptor, 0, len(passkeys))
	for _, passkey := range passkeys {
		id, err := passkeyIDEncoding.DecodeString(passkey.ID)
		if err != nil || len(id) == 0 {
			continue
		}
		descriptor := protocol.CredentialDescriptor{
			Type:         protocol.PublicKeyCredentialType,
			CredentialID: id,
		}
		for _, transport := range passkey.Transports {
			descriptor.Transport = append(descriptor.Transport, protocol.AuthenticatorTransport(transport))
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors
}

// handleMyPasskeyRegisterFinish verifies the attestation against the stored
// challenge and commits the credential. Registering a passkey raises the
// account's privilege floor, so — exactly like TOTP confirm — the current
// session is upgraded to second-factor-complete and every other session of
// the user is revoked (#1172 pattern).
func (s *managementState) handleMyPasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	_, user, ok := s.sessionUserForFactor(w, r)
	if !ok {
		return
	}
	var req struct {
		Name       string          `json:"name"`
		Credential json.RawMessage `json:"credential"`
	}
	if !decodeJSONRequest(w, r, &req) {
		return
	}
	if len(req.Credential) == 0 {
		writeError(w, "credential is required", http.StatusBadRequest)
		return
	}
	if len(req.Name) > maxPasskeyNameLen {
		writeError(w, "name is too long", http.StatusBadRequest)
		return
	}
	sessionToken := currentSessionToken(r)
	challenge, found := s.webAuthnChallenges().Take(sessionToken)
	if !found || challenge.Username != user.Username {
		writeError(w, "no pending passkey registration; begin first", http.StatusBadRequest)
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(req.Credential))
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.passkey.register", Target: user.Username,
			Success: false, Error: "malformed attestation",
		})
		writeError(w, "invalid passkey response", http.StatusBadRequest)
		return
	}
	wa, _, err := webAuthnForRequest(r)
	if err != nil {
		writeError(w, "passkey registration is unavailable", http.StatusInternalServerError)
		return
	}
	// The user row is re-read inside the mutation below; the ceremony user
	// here must carry the credentials as of challenge issuance so a
	// concurrent deletion cannot let a stale allow-list through.
	credential, err := wa.CreateCredential(webAuthnUser{username: user.Username, passkeys: user.Passkeys}, challenge.Session, parsed)
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.passkey.register", Target: user.Username,
			Success: false, Error: "attestation verification failed",
		})
		writeError(w, "invalid passkey response", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Passkey"
	}
	passkey := passkeyFromCredential(credential, name, s.loginBackoffTime())

	err = s.withMutation(func(mutation managementstate.Mutation) error {
		current, found := s.findUserLocked(user.Username)
		if !found {
			return errUserNotFound
		}
		// Authoritative cap re-check under the lock — the begin-time check
		// raced with nothing, a concurrent register could fill the slot.
		if len(current.Passkeys) >= maxPasskeysPerUser {
			return errPasskeyLimit
		}
		for _, existing := range current.Passkeys {
			if existing.ID == passkey.ID {
				return errPasskeyVerifyFailed
			}
		}
		// Journal the revocation intent BEFORE the factor commits so a crash
		// between commit and delete_many cannot leave pre-enrollment sessions
		// resurrectable at next load — the same #1059 pattern TOTP confirm
		// uses. Registering a credential raises the account's privilege floor:
		// sessions minted before it must re-authenticate.
		intent, intentErr := s.sessionRegistry().MarkUsernameRevocationPending(user.Username)
		if intentErr != nil {
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, intentErr)
		}
		update := current
		update.Passkeys = append(append([]model.Passkey(nil), current.Passkeys...), passkey)
		if _, mErr := mutation.SetUserPasskeys(user.Username, update); mErr != nil {
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return mErr
		}
		if _, markErr := s.sessionRegistry().MarkSecondFactorPersisted(sessionToken); markErr != nil {
			if _, restoreErr := mutation.SetUserPasskeys(user.Username, current); restoreErr != nil {
				return fmt.Errorf("%w: %v (restore user: %v)", errSessionRevocationPersistence, markErr, restoreErr)
			}
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, markErr)
		}
		if _, revokeErr := s.sessionRegistry().DeleteUsernameExceptPersisted(user.Username, sessionToken); revokeErr != nil {
			if _, restoreErr := mutation.SetUserPasskeys(user.Username, current); restoreErr != nil {
				return fmt.Errorf("%w: %v (restore user: %v)", errSessionRevocationPersistence, revokeErr, restoreErr)
			}
			_ = s.sessionRegistry().CancelUsernameRevocation(intent)
			return fmt.Errorf("%w: %v", errSessionRevocationPersistence, revokeErr)
		}
		if cancelErr := s.sessionRegistry().CancelUsernameRevocation(intent); cancelErr != nil {
			_ = cancelErr
		}
		return nil
	})
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.passkey.register", Target: user.Username,
			Success: false, Error: err.Error(),
		})
		switch {
		case errors.Is(err, errUserNotFound):
			writeNotFound(w)
		case errors.Is(err, errPasskeyVerifyFailed):
			writeError(w, "passkey already registered", http.StatusConflict)
		case errors.Is(err, errPasskeyLimit):
			writeError(w, errPasskeyLimit.Error(), http.StatusBadRequest)
		case errors.Is(err, errSessionRevocationPersistence):
			writeError(w, errSessionRevocationPersistence.Error(), http.StatusInternalServerError)
		default:
			writeError(w, "failed to persist passkey", http.StatusInternalServerError)
		}
		return
	}
	s.catchUpAfterPanelMutation()
	s.recordRequestAudit(r, audit.Record{
		Actor: user.Username, Action: "user.passkey.register", Target: user.Username,
		Success: true, Details: map[string]any{"id": passkey.ID},
	})
	writeJSONStatus(w, http.StatusCreated, passkeyListEntryOf(passkey))
}

// handleMyPasskeyByID serves DELETE /api/v1/users/me/passkeys/{id}. Removing
// a credential never revokes sessions (#1171): the auth middleware's
// factor-requirement check releases the mark requirement automatically once
// the last factor is gone.
func (s *managementState) handleMyPasskeyByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w, http.MethodDelete)
		return
	}
	session, user, ok := s.sessionUserForFactor(w, r)
	if !ok {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/users/me/passkeys/")
	passkeyID := strings.Trim(rest, "/")
	if passkeyID == "" || strings.Contains(passkeyID, "/") {
		writeNotFound(w)
		return
	}
	if _, err := passkeyIDEncoding.DecodeString(passkeyID); err != nil {
		writeNotFound(w)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if !decodeOptionalJSONRequest(w, r, &req) {
		return
	}
	if !s.passkeyCredentialGate(w, r, session, user, req.Password) {
		return
	}
	deleted := false
	err := s.withMutation(func(mutation managementstate.Mutation) error {
		current, found := s.findUserLocked(user.Username)
		if !found {
			return errUserNotFound
		}
		kept := make([]model.Passkey, 0, len(current.Passkeys))
		for _, existing := range current.Passkeys {
			if existing.ID == passkeyID {
				deleted = true
				continue
			}
			kept = append(kept, existing)
		}
		if !deleted {
			return errPasskeyNotFound
		}
		update := current
		update.Passkeys = kept
		_, mErr := mutation.SetUserPasskeys(user.Username, update)
		return mErr
	})
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Actor: user.Username, Action: "user.passkey.delete", Target: user.Username,
			Success: false, Error: err.Error(),
		})
		switch {
		case errors.Is(err, errUserNotFound), errors.Is(err, errPasskeyNotFound):
			writeNotFound(w)
		default:
			writeError(w, "failed to delete passkey", http.StatusInternalServerError)
		}
		return
	}
	s.catchUpAfterPanelMutation()
	s.recordRequestAudit(r, audit.Record{
		Actor: user.Username, Action: "user.passkey.delete", Target: user.Username,
		Success: true, Details: map[string]any{"id": passkeyID},
	})
	w.WriteHeader(http.StatusNoContent)
}

var errPasskeyNotFound = errors.New("passkey not found")

// handleV1UserFactorReset serves the admin factor-reset routes under
// /api/v1/users/{username}/: "totp" clears the TOTP factor (auth_totp.go) and
// "passkeys" clears every registered WebAuthn credential (#1171).
func (s *managementState) handleV1UserFactorReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w, http.MethodDelete)
		return
	}
	if !requestHasAdminRole(s, r) {
		writeError(w, "forbidden: admin role required", http.StatusForbidden)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		writeNotFound(w)
		return
	}
	switch parts[1] {
	case "totp":
		s.handleV1UserTOTPReset(w, r)
	case "passkeys":
		s.handleV1UserPasskeyReset(w, r, parts[0])
	default:
		writeNotFound(w)
	}
}

// handleV1UserPasskeyReset clears every passkey of the target user — the
// admin escape for a lost authenticator. Sessions are deliberately NOT
// revoked: unlike TOTP reset, dropping a possession factor cannot let an
// existing session bypass a still-armed factor, and the requirement says a
// passkey removal must never invalidate sessions (#1171).
func (s *managementState) handleV1UserPasskeyReset(w http.ResponseWriter, r *http.Request, username string) {
	err := s.withMutation(func(mutation managementstate.Mutation) error {
		current, found := s.findUserLocked(username)
		if !found {
			return errUserNotFound
		}
		if len(current.Passkeys) == 0 {
			return nil // already clean; keep reset idempotent
		}
		update := current
		update.ClearPasskeys()
		_, mErr := mutation.SetUserPasskeys(username, update)
		return mErr
	})
	if err != nil {
		s.recordRequestAudit(r, audit.Record{
			Action: "user.passkey.reset", Target: username, Success: false, Error: err.Error(),
		})
		if errors.Is(err, errUserNotFound) {
			writeNotFound(w)
			return
		}
		writeError(w, "failed to persist passkey reset", http.StatusInternalServerError)
		return
	}
	s.catchUpAfterPanelMutation()
	s.recordRequestAudit(r, audit.Record{
		Action: "user.passkey.reset", Target: username, Success: true,
	})
	w.WriteHeader(http.StatusNoContent)
}
