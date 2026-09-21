package naiveproxy

import (
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// Plugin implements the naiveproxy protocol.
type Plugin struct{}

// New creates a naiveproxy plugin instance.
func New() *Plugin { return &Plugin{} }

func (Plugin) Protocol() string        { return "naiveproxy" }
func (Plugin) DisplayName() string     { return "NaiveProxy" }
func (Plugin) Transports() []string    { return []string{"tcp"} }
func (Plugin) RequiresCaddy() bool     { return true }
func (Plugin) FirewallService() string { return "Veil NaiveProxy" }
func (Plugin) MaxEnabled() int         { return 0 }

// EnforcesPerClientCredentials reports that the Caddy forward_auth user list
// authenticates each client with its own credential (audit #309).
func (Plugin) EnforcesPerClientCredentials() bool { return true }

func naiveUsername(settings model.Settings, inbound model.Inbound) string {
	username := model.EffectiveProtocolString(inbound, settings, "naiveUsername", inbound.NaiveUsername, settings.NaiveUsername)
	if username == "" {
		username = model.DefaultNaiveUsername
	}
	return username
}

// naivePassword resolves the effective fallback password. The winning value is
// preserved byte-for-byte so the rendered Caddy user list and every exported
// client link carry identical credential bytes (audit #331).
func naivePassword(settings model.Settings, inbound model.Inbound) string {
	return model.EffectiveProtocolPassword(inbound, settings, "naivePassword", inbound.NaivePassword, settings.NaivePassword)
}

// NaiveDomain returns the public domain for the inbound, preferring the inbound
// ProtocolFields and falling back to the global settings domain. The result is
// trimmed and lowercased.
func NaiveDomain(settings model.Settings, inbound model.Inbound) string {
	return model.ResolveInboundDomain(inbound, settings)
}

// NaiveEmail returns the ACME contact email explicitly set on the inbound,
// trimmed of whitespace. It does not fall back to global settings; callers that
// need the effective email should resolve the domain-level chain themselves.
func NaiveEmail(_ model.Settings, inbound model.Inbound) string {
	return model.InboundEmail(inbound)
}

// NaivePublicPort returns the public port for the inbound, falling back to the
// inbound listen port, the global default inbound public port, and finally 443.
// The canonical resolver lives in model so firewall rules, client links and
// the bind plan all agree (audit #81/#128).
func NaivePublicPort(settings model.Settings, inbound model.Inbound) int {
	return model.ResolveNaivePublicPort(settings, inbound)
}

// NaiveTransport returns the transport from the inbound ProtocolFields,
// defaulting to "tcp" when unset.
func NaiveTransport(inbound model.Inbound) string {
	t := stringField(inbound.ProtocolFields, "transport")
	if t == "" {
		return "tcp"
	}
	return t
}

// NaiveFallbackRoot returns the fallback web root for the inbound, falling back
// to the inbound-level fallback root, the global fallback root, and finally the
// managed <etc>/www default resolved for this install (issue #634).
func NaiveFallbackRoot(settings model.Settings, inbound model.Inbound) string {
	root := stringField(inbound.ProtocolFields, "fallbackRoot")
	if root == "" {
		root = inbound.FallbackRoot
	}
	if root == "" {
		root = settings.FallbackRoot
	}
	if root == "" {
		root = DefaultFallbackRoot()
	}
	return root
}

// DefaultFallbackRoot resolves the managed naive fallback web root for this
// process: <etc>/www honoring VEIL_ETC_DIR/VEIL_LIVE_ROOT/VEIL_KEY_PATH with
// the packaged /etc/veil default.
func DefaultFallbackRoot() string {
	return filepath.ToSlash(filepath.Join(hostenv.EtcDir(), "www"))
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key].(string)
	if !ok {
		return ""
	}
	return v
}
