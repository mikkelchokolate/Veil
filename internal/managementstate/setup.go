package managementstate

import (
	"time"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// HasAdminUser reports whether snapshot users include an administrator.
func HasAdminUser(users []model.User) bool {
	for _, user := range users {
		if user.Role == "admin" {
			return true
		}
	}
	return false
}

// CompleteSetupForAdmins marks first-run setup complete when an administrator
// already exists. Install and `veil admin` create that user outside the SPA
// setup form; leaving setup.completed=false makes the persisted state lie
// about first-run even though login is already possible.
func CompleteSetupForAdmins(snapshot *model.ManagementSnapshot, at time.Time) {
	if snapshot == nil || !HasAdminUser(snapshot.Users) {
		return
	}
	snapshot.Setup.Completed = true
	if snapshot.Setup.CompletedAt == "" && !at.IsZero() {
		snapshot.Setup.CompletedAt = at.UTC().Format(time.RFC3339)
	}
}
