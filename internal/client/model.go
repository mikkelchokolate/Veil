// Package client implements the normalized Client domain entity: a durable
// identity decoupled from any single inbound, bound to one or more inbounds
// via ClientBinding, with encrypted per-binding credentials, quota/expiry
// policy, and optimistic-locking versioning. Runtime configs and subscriptions
// are rendered from this model, not from legacy inbound-embedded profiles.
package client

import "time"

// Quota reset policies.
const (
	ResetNever   = "never"
	ResetDaily   = "daily"
	ResetWeekly  = "weekly"
	ResetMonthly = "monthly"
)

// Client is the durable identity. ID is an immutable UUID; name is mutable and
// NOT an identifier; email is an optional contact field, never an identity.
type Client struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Email            *string `json:"email,omitempty"`
	Enabled          bool    `json:"enabled"`
	GroupID          *string `json:"groupId,omitempty"`
	QuotaBytes       *int64  `json:"quotaBytes,omitempty"`
	QuotaResetPolicy string  `json:"quotaResetPolicy"`
	QuotaResetAt     *int64  `json:"quotaResetAt,omitempty"`
	ExpiresAt        *int64  `json:"expiresAt,omitempty"`
	// DeviceLimit caps the number of concurrent sessions the client may hold.
	// IPLimit caps the number of distinct source IPs those sessions may come
	// from. Both are per-client (aggregate across the client's enabled
	// bindings), both are nullable (nil = unlimited), and both are enforced
	// only by protocols that advertise connection-limit enforcement
	// (currently Hysteria2, via its HTTP auth callback).
	DeviceLimit *int   `json:"deviceLimit,omitempty"`
	IPLimit     *int   `json:"ipLimit,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Depleted    bool   `json:"depleted"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
	Version     int    `json:"version"`
}

// Binding associates a client with one inbound. ProtocolSettings holds
// per-binding, protocol-specific non-secret options (JSON). One client may be
// bound to many inbounds; (client_id, inbound_id) is unique.
type Binding struct {
	ID               string `json:"id"`
	ClientID         string `json:"clientId"`
	InboundID        string `json:"inboundId"`
	RuntimeIdentity  string `json:"runtimeIdentity"`
	Enabled          bool   `json:"enabled"`
	ProtocolSettings string `json:"protocolSettings,omitempty"`
	CreatedAt        int64  `json:"createdAt"`
	UpdatedAt        int64  `json:"updatedAt"`
	Version          int    `json:"version"`
}

// Credential is encrypted credential material for a binding. The plaintext
// value is never stored, logged, or returned by list endpoints.
type Credential struct {
	ID                string `json:"id"`
	BindingID         string `json:"bindingId"`
	Kind              string `json:"kind"`
	EncryptedValue    []byte `json:"-"`
	KeyVersion        int    `json:"keyVersion"`
	CredentialVersion int    `json:"credentialVersion"`
	CreatedAt         int64  `json:"createdAt"`
	RotatedAt         *int64 `json:"rotatedAt,omitempty"`
	RevokedAt         *int64 `json:"revokedAt,omitempty"`
}

// EffectiveStatus is the deterministic, computed access state of a client.
// Priority order (highest first) is documented in ComputeStatus.
type EffectiveStatus string

const (
	StatusApplyFailed          EffectiveStatus = "apply_failed"
	StatusExpired              EffectiveStatus = "expired"
	StatusDepleted             EffectiveStatus = "depleted"
	StatusDisabled             EffectiveStatus = "disabled"
	StatusPendingApply         EffectiveStatus = "pending_apply"
	StatusActive               EffectiveStatus = "active"
	StatusUnsupportedTelemetry EffectiveStatus = "unsupported_telemetry"
	StatusOrphaned             EffectiveStatus = "orphaned"
)

// ComputeStatus resolves the effective status with a deterministic priority:
//
//	apply_failed > expired > depleted > disabled > pending_apply > orphaned > active
//
// Inputs are booleans describing the client's current situation. The exact
// rule is fixed here and covered by tests so the UI and API agree.
func ComputeStatus(c Client, now time.Time, applyFailed, pendingApply, orphaned bool) EffectiveStatus {
	switch {
	case applyFailed:
		return StatusApplyFailed
	case c.ExpiresAt != nil && now.Unix() >= *c.ExpiresAt:
		// Any non-null expiry at or before now is expired — including the
		// <=0 values legacy rows can still carry. This matches the runtime
		// binding filter, the render filter and the expiration reconciler,
		// which all treat expires_at<=now as expired (#1108).
		return StatusExpired
	case c.Depleted:
		return StatusDepleted
	case !c.Enabled:
		return StatusDisabled
	case pendingApply:
		return StatusPendingApply
	case orphaned:
		return StatusOrphaned
	default:
		return StatusActive
	}
}

// RuntimeEligible reports whether a client may still be admitted to a
// protocol runtime: enabled, not quota-depleted, and unexpired
// (expires_at > now). It is the single gate behind render admission —
// ListRuntimeBindingsForInbound applies the same rule in SQL and the
// pinned snapshot render checks it per client — and every consumer that
// reads runtime telemetry (presence, traffic identity attribution) must
// agree with it: a client the renderer excluded can leave only residual
// telemetry, which must never prove "online" or attribute traffic.
func RuntimeEligible(enabled, depleted bool, expiresAt *int64, now int64) bool {
	return enabled && !depleted && (expiresAt == nil || *expiresAt > now)
}

// RuntimeEligible is the Client receiver form of the package-level gate.
func (c Client) RuntimeEligible(now int64) bool {
	return RuntimeEligible(c.Enabled, c.Depleted, c.ExpiresAt, now)
}

// nowUnix is a seam for tests.
var nowUnix = func() int64 { return time.Now().Unix() }

func timeFromUnix(sec int64) time.Time { return time.Unix(sec, 0) }
