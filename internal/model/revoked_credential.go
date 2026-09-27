package model

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// RevokedClientCredential returns a deterministic sentinel username/password
// for an inbound whose normalized clients were all revoked (or whose profiles
// are all disabled). Caddy's forward_proxy module treats an EMPTY
// auth_credentials list as "no authentication" — an open relay — and a
// Hysteria2 userpass map must likewise never be empty while the service keeps
// running, so a credential-managed inbound with zero usable credentials still
// renders one account that can never authenticate (issue #1098).
//
// The password is derived with HMAC-SHA256 keyed by per-install secret
// material: Settings.CredentialDerivationSecret (injected at state load /
// snapshot build from the management-state key) plus whatever credential
// fields the inbound/settings still carry. HMAC is used deliberately: this is
// a deterministic key-derivation for config rendering — the value must be
// byte-stable across renders so generated configs do not churn and services
// do not reload spuriously — not a password-storage hash, and not a hash of
// publicly known data (the key always contains a 256-bit per-install secret
// whenever managed state exists).
//
// Fail-closed residual: if NO secret material exists at all (no
// CredentialDerivationSecret and every credential field empty — reachable
// only in render contexts without managed state, e.g. ad-hoc unit tests), the
// sentinel falls back to a random password so it stays unguessable; a
// publicly computable sentinel would let anyone authenticate as
// "veil-revoked-<name>" and defeat the revocation.
func RevokedClientCredential(settings Settings, inbound Inbound) (username, password string) {
	username = "veil-revoked-" + inbound.Name

	// Credential material fields — real secrets when set. Collect them so the
	// "is anything secret available" check is explicit.
	credentialFields := []string{
		settings.CredentialDerivationSecret,
		inbound.Password,
		inbound.NaivePassword,
		settings.NaivePassword,
		protocolFieldString(inbound.ProtocolFields, "naivePassword"),
		protocolFieldString(inbound.ProtocolFields, "password"),
	}
	key := strings.Join(credentialFields, "\x00")
	if strings.Trim(key, "\x00") == "" {
		randomBytes := make([]byte, 32)
		if _, err := rand.Read(randomBytes); err != nil {
			// crypto/rand failure: emitting any constant here would be a
			// publicly known password. Failing the render is the only
			// fail-closed outcome.
			panic("model: crypto/rand unavailable for revoked credential sentinel")
		}
		return username, hex.EncodeToString(randomBytes)
	}

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("veil-revoked-credential\x00"))
	mac.Write([]byte(inbound.Name))
	return username, hex.EncodeToString(mac.Sum(nil))
}

func protocolFieldString(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	v, ok := fields[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
