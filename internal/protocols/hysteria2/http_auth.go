package hysteria2

import (
	"encoding/hex"
	"net/url"

	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
	"golang.org/x/crypto/argon2"
)

// HTTPAuthPathPrefix is the path prefix the panel's internal auth listener
// serves: <prefix><inbound>/<secret>. The inbound segment scopes which
// inbound's credential set applies; the secret segment authenticates the
// daemon itself (the URL inside the rendered config is otherwise the only
// thing that knows it). internal/api mounts the handler under this exact
// prefix — the renderer and the handler must never drift apart.
const HTTPAuthPathPrefix = "/api/internal/hy2-auth/"

// HTTPAuthSecret derives the per-inbound shared secret embedded in the
// rendered auth.http.url. The KDF input mixes the inbound's effective shared
// password with Settings.CredentialDerivationSecret — the per-install secret
// derived from the management-state encryption key — so an empty or weak
// shared password cannot leave the callback path publicly computable or
// cheaply guessable (issue #1205). It is a separate derivation domain from
// the traffic-stats secret so leaking one cannot reveal the other, and it is
// stable across restarts so a rendered config keeps authenticating the unit
// it was written for. It never appears in public API output — only in the
// on-disk rendered YAML, which is already treated as secret material.
//
// Residual: render contexts without a per-install secret (ad-hoc callers
// that never loaded managed state — the panel itself always has one) fall
// back to password-only derivation, matching the pre-#1205 behavior.
func HTTPAuthSecret(settings model.Settings, inbound model.Inbound) string {
	password := hysteria2Password(settings, inbound)
	material := password
	if settings.CredentialDerivationSecret != "" {
		material = settings.CredentialDerivationSecret + "\x00" + password
	}
	salt := []byte("veil-hysteria2-http-auth\x00" + inbound.Name)
	digest := argon2.IDKey(
		[]byte(material),
		salt,
		trafficStatsArgonTime,
		trafficStatsArgonMemory,
		trafficStatsArgonThreads,
		trafficStatsSecretBytes,
	)
	return hex.EncodeToString(digest)
}

// SharedPassword exposes the effective inbound fallback password so the
// internal auth callback can keep shared-password authentication working
// byte-for-byte for inbounds that have no per-client credentials.
func SharedPassword(settings model.Settings, inbound model.Inbound) string {
	return hysteria2Password(settings, inbound)
}

// HTTPAuthURL is the full callback URL rendered into auth.http.url. The
// listener lives inside the 127.40.0.0/16 band the unit egress filter
// pierces; TLS would buy nothing on a fixed loopback hop and would need
// cert plumbing inside the unit, so plain HTTP + the per-inbound secret is
// the transport contract.
func HTTPAuthURL(settings model.Settings, inbound model.Inbound) string {
	return "http://" + runtimeports.Hysteria2HTTPAuthAddress() +
		HTTPAuthPathPrefix + url.PathEscape(inbound.Name) + "/" +
		HTTPAuthSecret(settings, inbound)
}
