package managementstate

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestMutationUpdateUserPreservesPasswordHashWhenOmitted(t *testing.T) {
	users := []model.User{{
		Username:     "viewer",
		PasswordHash: "current-password-hash",
		Role:         "viewer",
		Locale:       "en",
	}}
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Users: &users}, nil)

	updated, err := mutation.UpdateUser("viewer", model.User{
		Username: "viewer",
		Role:     "admin",
		Locale:   "ru",
	})
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}
	if updated.PasswordHash != "current-password-hash" {
		t.Fatalf("updated password hash = %q", updated.PasswordHash)
	}
	if users[0].PasswordHash != "current-password-hash" {
		t.Fatalf("stored password hash = %q", users[0].PasswordHash)
	}
	if users[0].Role != "admin" || users[0].Locale != "ru" {
		t.Fatalf("non-password fields were not updated: %+v", users[0])
	}
}

func TestMutationUpdateUserReplacesExplicitPasswordHash(t *testing.T) {
	users := []model.User{{
		Username:     "viewer",
		PasswordHash: "old-password-hash",
		Role:         "viewer",
		Locale:       "en",
	}}
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Users: &users}, nil)

	updated, err := mutation.UpdateUser("viewer", model.User{
		Username:     "viewer",
		PasswordHash: "new-password-hash",
		Role:         "viewer",
		Locale:       "en",
	})
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}
	if updated.PasswordHash != "new-password-hash" || users[0].PasswordHash != "new-password-hash" {
		t.Fatalf("explicit password hash was not applied: updated=%q stored=%q", updated.PasswordHash, users[0].PasswordHash)
	}
}
