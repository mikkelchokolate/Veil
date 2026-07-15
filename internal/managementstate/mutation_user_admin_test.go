package managementstate

import (
	"errors"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestMutationRejectsDemotingLastAdministrator(t *testing.T) {
	users := []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin", Locale: "en"}}
	saves := 0
	mutation := NewMutation(MutationTarget{Users: &users}, func() error {
		saves++
		return nil
	})

	_, err := mutation.UpdateUser("admin", model.User{PasswordHash: "hash", Role: "viewer", Locale: "en"})
	if !errors.Is(err, ErrLastAdministrator) {
		t.Fatalf("UpdateUser error = %v, want ErrLastAdministrator", err)
	}
	if users[0].Role != "admin" {
		t.Fatalf("last administrator role changed to %q", users[0].Role)
	}
	if saves != 0 {
		t.Fatalf("saves = %d, want 0", saves)
	}
}

func TestMutationRejectsDeletingLastAdministrator(t *testing.T) {
	users := []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin", Locale: "en"}}
	mutation := NewMutation(MutationTarget{Users: &users}, func() error { return nil })

	if err := mutation.DeleteUser("admin"); !errors.Is(err, ErrLastAdministrator) {
		t.Fatalf("DeleteUser error = %v, want ErrLastAdministrator", err)
	}
	if len(users) != 1 || users[0].Username != "admin" {
		t.Fatalf("last administrator was deleted: %+v", users)
	}
}

func TestMutationAllowsAdministratorChangeWhenAnotherAdminRemains(t *testing.T) {
	users := []model.User{
		{Username: "alice", PasswordHash: "hash", Role: "admin", Locale: "en"},
		{Username: "bob", PasswordHash: "hash", Role: "admin", Locale: "en"},
	}
	mutation := NewMutation(MutationTarget{Users: &users}, func() error { return nil })

	updated, err := mutation.UpdateUser("alice", model.User{PasswordHash: "hash", Role: "viewer", Locale: "en"})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if updated.Role != "viewer" {
		t.Fatalf("updated role = %q, want viewer", updated.Role)
	}
	if err := mutation.DeleteUser("bob"); !errors.Is(err, ErrLastAdministrator) {
		t.Fatalf("deleting the remaining admin error = %v, want ErrLastAdministrator", err)
	}
}
