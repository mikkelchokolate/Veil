package api

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// WebAuthn is implemented as a second factor for panel login (#1171) stacked
// on the pending_2fa machinery of #1172: the login stage advertises "webauthn"
// among secondFactorMethods, POST /api/v1/auth/webauthn/begin mints an
// assertion challenge bound to the pending cookie, and
// POST /api/v1/auth/webauthn/finish completes the pending challenge exactly
// like the TOTP verify endpoint does.

// webAuthnRPDisplayName is the human-palatable relying-party name shown by
// browsers and authenticators during ceremonies.
const webAuthnRPDisplayName = "Veil"

// webAuthnChallengeTTL mirrors pendingSecondFactorTTL: a ceremony challenge is
// credential-grade material and must die fast. It also bounds the lifetime of
// the in-memory record the challenge store keeps.
const webAuthnChallengeTTL = pendingSecondFactorTTL

// maxWebAuthnChallenges bounds the ceremony-challenge map. Registration
// challenges are keyed by session token and login challenges by the pending
// token, but both maps share this ceiling so unauthenticated or runaway
// clients cannot grow memory without limit.
const maxWebAuthnChallenges = 4096

var errWebAuthnUnavailable = errors.New("webauthn relying party could not be configured for this request")

// passkeyIDEncoding is the canonical string form of a credential ID inside
// model.Passkey and in the me/passkeys/{id} path parameter: base64url without
// padding, so it is URL-safe by construction.
var passkeyIDEncoding = base64.RawURLEncoding

// webAuthnConfiguredDomain returns the panel's public host as configured:
// the dedicated panel domain under caddy-fronted access, else the site
// domain — the same source publicSubscriptionURL uses (#1222).
func (s *managementState) webAuthnConfiguredDomain() string {
	s.mu.Lock()
	settings := s.settings
	s.mu.Unlock()
	domain := strings.Trim(strings.TrimSpace(settings.Domain), "[]")
	if strings.EqualFold(strings.TrimSpace(settings.PanelAccess), "caddy") {
		if panel := strings.Trim(strings.TrimSpace(settings.PanelDomain), "[]"); panel != "" {
			domain = panel
		}
	}
	// Tolerate a host[:port] being configured: the RP ID is a bare host.
	if host, _, err := net.SplitHostPort(domain); err == nil {
		domain = strings.Trim(host, "[]")
	}
	return strings.ToLower(domain)
}

// webAuthnHostWithin reports whether host equals the RP domain or sits under
// it as a subdomain — the Host allow-list check a configured RP applies to
// ceremony requests (#1222).
func webAuthnHostWithin(host, rpID string) bool {
	return host == rpID || strings.HasSuffix(host, "."+rpID)
}

// webAuthnForRequest builds the relying-party config. When a panel domain is
// configured it PINS the RP ID: credentials then bind to one stable origin
// family regardless of which Host alias served the request, and a request
// Host outside the configured domain tree is refused — it could only mint
// credentials the canonical origin can never use anyway (#1222). Without a
// configured domain the RP ID falls back to the request Host so the same
// code serves a direct https://panel:8443 panel and unconfigured dev panels.
// Origins are https://<host:port> for every admitted host, plus http://
// loopback origins for local development (the only plain-HTTP origins
// browsers will run WebAuthn on anyway — a secure-context requirement, not a
// policy we can weaken).
func (s *managementState) webAuthnForRequest(r *http.Request) (*webauthn.WebAuthn, string, error) {
	hostPort := r.Host
	if hostPort == "" && r.URL != nil {
		hostPort = r.URL.Host
	}
	requestHost := hostPort
	if host, _, err := net.SplitHostPort(hostPort); err == nil {
		requestHost = host
	}
	// SplitHostPort leaves IPv6 brackets off; a bare-literal request line
	// can still carry them, so normalize both directions.
	requestHost = strings.ToLower(strings.TrimPrefix(strings.TrimSuffix(requestHost, "]"), "["))
	rpID := requestHost
	var origins []string
	if configured := s.webAuthnConfiguredDomain(); configured != "" {
		if !webAuthnHostWithin(requestHost, configured) {
			return nil, "", errWebAuthnUnavailable
		}
		rpID = configured
		origins = []string{"https://" + configured}
		if hostPort != "" && !strings.EqualFold(hostPort, configured) {
			// The request's own host:port (validated above to sit inside
			// the RP domain) covers subdomain and non-standard-port access.
			origins = append(origins, "https://"+hostPort)
		}
	} else {
		if rpID == "" {
			return nil, "", errWebAuthnUnavailable
		}
		origins = []string{"https://" + hostPort}
	}
	if rpID == "localhost" || rpID == "127.0.0.1" || rpID == "::1" {
		// Dev mode: the browser may reach the panel at either loopback name.
		// The port belongs to the request — a vite dev server on :5173 and a
		// built panel on :2096 never share a ceremony.
		port := ""
		if _, p, err := net.SplitHostPort(hostPort); err == nil {
			port = p
		}
		suffix := ""
		if port != "" {
			suffix = ":" + port
		}
		origins = append(origins,
			"http://"+hostPort,
			"http://localhost"+suffix,
			"http://127.0.0.1"+suffix,
		)
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          rpID,
		RPDisplayName: webAuthnRPDisplayName,
		RPOrigins:     origins,
		// Second-factor ceremonies never need attestation trust-chain
		// validation — the credential is only ever a possession factor for
		// this RP. Keeping the "none" preference also avoids leaking the
		// authenticator model into panel state.
		AttestationPreference: protocol.PreferNoAttestation,
	})
	if err != nil {
		return nil, "", err
	}
	return wa, rpID, nil
}

// webAuthnUser adapts model.User to the library's webauthn.User interface.
type webAuthnUser struct {
	username string
	passkeys []model.Passkey
}

// WebAuthnID is the opaque user handle (max 64 bytes per spec). A SHA-256 of
// the username is stable across password/role changes, opaque on the wire,
// and never leaks the account name into the credential record.
func (u webAuthnUser) WebAuthnID() []byte {
	sum := sha256.Sum256([]byte(u.username))
	return sum[:]
}

func (u webAuthnUser) WebAuthnName() string        { return u.username }
func (u webAuthnUser) WebAuthnDisplayName() string { return u.username }

func (u webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	credentials := make([]webauthn.Credential, 0, len(u.passkeys))
	for _, passkey := range u.passkeys {
		if credential, ok := passkeyToCredential(passkey); ok {
			credentials = append(credentials, credential)
		}
	}
	return credentials
}

// passkeyToCredential converts a stored passkey row into the library's
// credential record. Corrupt rows (undecodable fields) are skipped rather
// than trusted — WebAuthnCredentials feeding a half-valid credential into
// ValidateLogin could turn a state-file inconsistency into an auth bypass.
func passkeyToCredential(passkey model.Passkey) (webauthn.Credential, bool) {
	id, err := passkeyIDEncoding.DecodeString(passkey.ID)
	if err != nil || len(id) == 0 {
		return webauthn.Credential{}, false
	}
	publicKey, err := base64.StdEncoding.DecodeString(passkey.PublicKey)
	if err != nil || len(publicKey) == 0 {
		return webauthn.Credential{}, false
	}
	credential := webauthn.Credential{
		ID:                id,
		PublicKey:         publicKey,
		AttestationType:   passkey.AttestationType,
		AttestationFormat: passkey.AttestationFormat,
		Authenticator: webauthn.Authenticator{
			SignCount:    passkey.SignCount,
			CloneWarning: passkey.CloneWarning,
			Attachment:   protocol.AuthenticatorAttachment(passkey.Attachment),
		},
		Flags: webauthn.CredentialFlags{
			UserPresent:    true,
			UserVerified:   passkey.UserVerified,
			BackupEligible: passkey.BackupEligible,
			BackupState:    passkey.BackupState,
		},
	}
	if aaguid, err := base64.StdEncoding.DecodeString(passkey.AAGUID); err == nil && len(aaguid) > 0 {
		credential.Authenticator.AAGUID = aaguid
	}
	for _, transport := range passkey.Transports {
		credential.Transport = append(credential.Transport, protocol.AuthenticatorTransport(transport))
	}
	return credential, true
}

// passkeyFromCredential renders a freshly created/updated library credential
// into its stored form. createdAt is RFC3339; existing passkeys keep their
// original timestamp on sign-count write-back.
func passkeyFromCredential(credential *webauthn.Credential, name string, createdAt time.Time) model.Passkey {
	passkey := model.Passkey{
		ID:                passkeyIDEncoding.EncodeToString(credential.ID),
		Name:              name,
		PublicKey:         base64.StdEncoding.EncodeToString(credential.PublicKey),
		AttestationType:   credential.AttestationType,
		AttestationFormat: credential.AttestationFormat,
		SignCount:         credential.Authenticator.SignCount,
		CloneWarning:      credential.Authenticator.CloneWarning,
		UserVerified:      credential.Flags.UserVerified,
		BackupEligible:    credential.Flags.BackupEligible,
		BackupState:       credential.Flags.BackupState,
		Attachment:        string(credential.Authenticator.Attachment),
		CreatedAt:         createdAt.UTC().Format(time.RFC3339),
	}
	if len(credential.Authenticator.AAGUID) > 0 {
		passkey.AAGUID = base64.StdEncoding.EncodeToString(credential.Authenticator.AAGUID)
	}
	for _, transport := range credential.Transport {
		passkey.Transports = append(passkey.Transports, string(transport))
	}
	return passkey
}

// passkeyApplyAssertion copies the fields a successful (or clone-flagged)
// assertion mutates back onto the stored row: sign counter, clone warning,
// and the latchable flags. The credential ID and public key never change.
func passkeyApplyAssertion(passkey *model.Passkey, credential *webauthn.Credential) {
	passkey.SignCount = credential.Authenticator.SignCount
	passkey.CloneWarning = credential.Authenticator.CloneWarning
	passkey.UserVerified = credential.Flags.UserVerified
	passkey.BackupState = credential.Flags.BackupState
	if att := string(credential.Authenticator.Attachment); att != "" {
		passkey.Attachment = att
	}
}

// webAuthnChallenge is the stored half of a Begin* call: the full SessionData
// must reach the matching Finish/Validate verbatim, so it is kept server-side
// and keyed by a credential-grade secret the caller already holds (the
// veil_session token for registration, the veil_pending_2fa token for login).
type webAuthnChallenge struct {
	Username  string
	Session   webauthn.SessionData
	ExpiresAt time.Time
}

// webAuthnChallengeStore is the bounded, single-use challenge table. Take is
// consume-on-read: a ceremony response may never be evaluated twice, and a
// failed finish must start over at begin rather than replaying the challenge.
type webAuthnChallengeStore struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]webAuthnChallenge
}

func newWebAuthnChallengeStore(now func() time.Time) *webAuthnChallengeStore {
	if now == nil {
		now = time.Now
	}
	return &webAuthnChallengeStore{
		now:     now,
		entries: make(map[string]webAuthnChallenge),
	}
}

// Put records a fresh ceremony challenge keyed by secret (already the caller's
// session or pending token — hashed before use as a map key). A previous
// challenge for the same key is silently replaced: re-beginning a ceremony
// is always legal and must not wedge the caller.
func (s *webAuthnChallengeStore) Put(secret, username string, session webauthn.SessionData) {
	if secret == "" {
		return
	}
	key := hashSessionSecret(secret)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	for k, entry := range s.entries {
		if !entry.ExpiresAt.After(now) {
			delete(s.entries, k)
		}
	}
	for len(s.entries) >= maxWebAuthnChallenges {
		oldestKey, oldest := "", time.Time{}
		for k, entry := range s.entries {
			if oldestKey == "" || entry.ExpiresAt.Before(oldest) {
				oldestKey, oldest = k, entry.ExpiresAt
			}
		}
		delete(s.entries, oldestKey)
	}
	s.entries[key] = webAuthnChallenge{
		Username:  username,
		Session:   session,
		ExpiresAt: now.Add(webAuthnChallengeTTL),
	}
}

// Take resolves AND consumes the challenge for secret. Unknown and expired
// keys both resolve to !ok — the caller fails closed with a generic denial.
func (s *webAuthnChallengeStore) Take(secret string) (webAuthnChallenge, bool) {
	if secret == "" {
		return webAuthnChallenge{}, false
	}
	key := hashSessionSecret(secret)
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok {
		return webAuthnChallenge{}, false
	}
	delete(s.entries, key)
	if !entry.ExpiresAt.After(s.now().UTC()) {
		return webAuthnChallenge{}, false
	}
	return entry, true
}

// Drop removes a challenge without consuming it — used when the caller
// learns mid-ceremony that the account changed and the challenge must not
// outlive its authorization.
func (s *webAuthnChallengeStore) Drop(secret string) {
	if secret == "" {
		return
	}
	s.mu.Lock()
	delete(s.entries, hashSessionSecret(secret))
	s.mu.Unlock()
}

// webAuthnChallenges lazily binds the ceremony store onto the management
// state, sharing the auth clock with pending_2fa so tests steer both from one
// variable. In-memory only: a restart forces ceremonies to begin again.
func (s *managementState) webAuthnChallenges() *webAuthnChallengeStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.webAuthnCeremonies == nil {
		s.webAuthnCeremonies = newWebAuthnChallengeStore(s.loginBackoffTime)
	}
	return s.webAuthnCeremonies
}
