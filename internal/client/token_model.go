package client

import "time"

// SubscriptionToken is an unguessable, revocable, rotatable capability token
// that grants read access to one client's subscription artifact. The hash is
// used for lookup. When a cipher is configured the panel also stores an
// encrypted copy so an administrator can re-open the subscription URL and QR.
type SubscriptionToken struct {
	ID         string `json:"id"`
	ClientID   string `json:"clientId"`
	TokenHash  string `json:"-"` // never serialized
	Prefix     string `json:"prefix"`
	Label      string `json:"label,omitempty"`
	CreatedBy  string `json:"createdBy,omitempty"`
	Enabled    bool   `json:"enabled"`
	ExpiresAt  *int64 `json:"expiresAt,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
	RotatedAt  *int64 `json:"rotatedAt,omitempty"`
	RevokedAt  *int64 `json:"revokedAt,omitempty"`
	LastUsedAt *int64 `json:"lastUsedAt,omitempty"`
	HasSecret  bool   `json:"hasSecret,omitempty"`
}

// IsActive reports whether the token currently grants access.
func (t SubscriptionToken) IsActive(now time.Time) bool {
	if t.RevokedAt != nil || !t.Enabled {
		return false
	}
	if t.ExpiresAt != nil && now.Unix() >= *t.ExpiresAt {
		return false
	}
	return true
}
