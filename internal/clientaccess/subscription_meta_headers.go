package clientaccess

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// SubscriptionMetaHeaders extends the base delivery headers with the
// subscription metadata that proxy clients (Clash, v2rayNG, sing-box,
// Streisand) consume: subscription-userinfo for traffic/quota display,
// profile-update-interval for auto-refresh cadence, and optional announce /
// support-url for client UI integration.
type SubscriptionMetaHeaders struct {
	// SubscriptionUserinfo carries "upload=N; download=N; total=N; expire=TS".
	// Nil fields are omitted so the client hides unknown values rather than
	// showing misleading zeros.
	Upload   int64
	Download int64
	Total    *int64 // quota bytes; nil = unlimited
	Expire   *int64 // unix seconds; nil = no expiry

	// ProfileUpdateIntervalHours tells the client how often to refresh.
	// 0 disables the header.
	ProfileUpdateIntervalHours int

	// Announce is an optional message shown by supporting clients.
	Announce string
	// SupportURL is an optional help/support link.
	SupportURL string
	// ProfileTitle names the profile in supporting clients.
	ProfileTitle string
}

func (h SubscriptionMetaHeaders) Apply(header http.Header) {
	var parts []string
	parts = append(parts, "upload="+strconv.FormatInt(h.Upload, 10))
	parts = append(parts, "download="+strconv.FormatInt(h.Download, 10))
	if h.Total != nil {
		parts = append(parts, "total="+strconv.FormatInt(*h.Total, 10))
	}
	if h.Expire != nil {
		parts = append(parts, "expire="+strconv.FormatInt(*h.Expire, 10))
	}
	header.Set("Subscription-Userinfo", strings.Join(parts, "; "))
	if h.ProfileUpdateIntervalHours > 0 {
		header.Set("Profile-Update-Interval", strconv.Itoa(h.ProfileUpdateIntervalHours))
	}
	if h.Announce != "" {
		header.Set("Announce", h.Announce)
	}
	if h.SupportURL != "" {
		header.Set("Support-URL", h.SupportURL)
	}
	if h.ProfileTitle != "" {
		header.Set("Profile-Title", h.ProfileTitle)
		header.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, sanitizeFilename(h.ProfileTitle)+".txt"))
	}
}

func sanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '"' || r == ';' {
			return '-'
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "veil-subscription"
	}
	return name
}
