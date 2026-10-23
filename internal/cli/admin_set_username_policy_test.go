package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// `veil admin set --username X` used to append any X as a brand-new Panel
// user, bypassing the username policy the API enforces on setup and on
// POST /api/users (#1266). A CLI-created "me" is shadowed by the
// /api/v1/users/me/* self-service routes, so an admin can never reset its
// second factor over the API; "a/b" can never be addressed by
// /api/users/{username}; control characters reach logs and the SPA verbatim.
func TestAdminSetRejectsNewUsernameOutsidePanelPolicy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		username string
	}{
		{name: "self-service alias me", username: "me"},
		{name: "self-service alias ME", username: "ME"},
		{name: "too short", username: "ab"},
		{name: "path separator", username: "ops/admin"},
		{name: "space", username: "ops admin"},
		{name: "leading space shadowing existing admin", username: " admin"},
		{name: "newline", username: "ops\nadmin"},
		{name: "longer than 64 bytes", username: strings.Repeat("a", 65)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "state.json")
			keyPath := filepath.Join(t.TempDir(), "state.key")
			writeAdminState(t, statePath, keyPath, []model.User{
				{Username: "admin", PasswordHash: "old", Role: "admin"},
			})

			cmd := newFastRootCommand("test")
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{
				"admin", "set",
				"--username", tc.username,
				"--password", "new-password-123",
				"--state", statePath,
				"--key-path", keyPath,
			})
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "username must be 3-64 characters") {
				t.Fatalf("admin set --username %q: want username policy error, got %v\n%s", tc.username, err, out.String())
			}
			users := loadAdminUsers(t, statePath, keyPath)
			if len(users) != 1 || users[0].Username != "admin" || users[0].PasswordHash != "old" {
				t.Fatalf("state changed after rejected username %q: %+v", tc.username, users)
			}
		})
	}
}

// The policy gates only the creation of a new account. An operator must still
// be able to recover a pre-existing account whose name predates the policy —
// the CLI is the only path that can reach a legacy "me" user at all.
func TestAdminSetStillUpdatesExistingLegacyUsername(t *testing.T) {
	for _, legacy := range []string{"me", "ops admin"} {
		t.Run(legacy, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "state.json")
			keyPath := filepath.Join(t.TempDir(), "state.key")
			writeAdminState(t, statePath, keyPath, []model.User{
				{Username: "admin", PasswordHash: "old-admin", Role: "admin"},
				{Username: legacy, PasswordHash: "old-legacy", Role: "viewer", Locale: "ru"},
			})

			cmd := newFastRootCommand("test")
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs([]string{
				"admin", "set",
				"--username", legacy,
				"--password", "new-password-123",
				"--state", statePath,
				"--key-path", keyPath,
			})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("admin set on existing %q: %v\n%s", legacy, err, out.String())
			}
			users := loadAdminUsers(t, statePath, keyPath)
			if len(users) != 2 {
				t.Fatalf("users = %+v", users)
			}
			got := users[1]
			if got.Username != legacy || got.Role != "viewer" || got.Locale != "ru" || got.PasswordHash == "old-legacy" {
				t.Fatalf("legacy user not updated in place: %+v", got)
			}
		})
	}
}
