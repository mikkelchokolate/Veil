package mieru

import (
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// ValidateSettings is a no-op for Mieru global settings.
func (Plugin) ValidateSettings(model.Settings, model.Inbound) error { return nil }

// ValidateInbound is a no-op for Mieru-specific inbound checks. The common
// inbound validator owns the generic 1..65535 port constraint. Pinned mita
// v3.36.1 accepts that full range in FlatPortBindings, including privileged
// ports; Veil's systemd unit grants CAP_NET_BIND_SERVICE for those ports.
func (Plugin) ValidateInbound(model.Settings, model.Inbound) []model.ValidationIssue { return nil }

// NeedsDomain reports that Mieru does not require a public domain.
func (Plugin) NeedsDomain(model.Settings, model.Inbound) bool { return false }

// NeedsEmail reports that Mieru does not need an email address.
func (Plugin) NeedsEmail(model.Settings, model.Inbound) bool { return false }

// HasCredential reports whether the inbound has a usable Mieru credential.
// The inbound-name/password fallback is used only when the inbound has no
// client profiles. If profiles exist and all are disabled, leftover inbound
// password must not count as a live user.
func (Plugin) HasCredential(settings model.Settings, inbound model.Inbound) bool {
	for _, profile := range inbound.Profiles {
		if profile.Enabled && strings.TrimSpace(profile.Password) != "" {
			return true
		}
	}
	for _, credential := range inbound.RuntimeCredentials {
		if strings.TrimSpace(credential.Password) != "" {
			return true
		}
	}
	// All-disabled profiles, migration-suppressed profiles, and normalized
	// bindings with no usable credential are all deliberate revocation
	// states — the legacy inbound password does not count as a live user
	// (issues #1098, #1117).
	if inbound.HadClientProfiles() {
		return false
	}
	return strings.TrimSpace(model.EffectiveInboundPassword(inbound)) != ""
}
