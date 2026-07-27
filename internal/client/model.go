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
	DeviceLimit      *int    `json:"deviceLimit,omitempty"`
	Notes            string  `json:"notes,omitempty"`
	Depleted         bool    `json:"depleted"`
	CreatedAt        int64   `json:"createdAt"`
	UpdatedAt        int64   `json:"updatedAt"`
	Version          int     `json:"version"`
}

// Binding associates a client with one inbound. ProtocolSettings holds
// per-binding, protocol-specific non-secret options (JSON). One client may be
// bound to many inbounds; (client_id, inbound_id) is unique.
type Binding struct {
	ID               string `json:"id"`
	ClientID         string `json:"clientId"`
	InboundID        string `json:"inboundId"`
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
	case c.ExpiresAt != nil && *c.ExpiresAt > 0 && now.Unix() >= *c.ExpiresAt:
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

// nowUnix is a seam for tests.
var nowUnix = func() int64 { return time.Now().Unix() }

func timeFromUnix(sec int64) time.Time { return time.Unix(sec, 0) }
