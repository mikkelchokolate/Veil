package managementstate

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

var errMutationSaveFailed = errors.New("mutation save failed")

func failingMutationSave() error {
	return errMutationSaveFailed
}

func TestMutationRollsBackSettingsWhenSaveFails(t *testing.T) {
	before := Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "before.example"}
	settings := before
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Settings: &settings}, failingMutationSave)

	_, err := mutation.UpdateSettings(Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "after.example"})
	if !errors.Is(err, errMutationSaveFailed) {
		t.Fatalf("UpdateSettings error = %v", err)
	}
	if !reflect.DeepEqual(settings, before) {
		t.Fatalf("settings changed after failed save: got=%+v want=%+v", settings, before)
	}
}

func TestMutationRollsBackInboundsWhenSaveFails(t *testing.T) {
	before := []Inbound{{Name: "existing", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true, Password: "secret"}}
	inbounds := append([]Inbound(nil), before...)
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Inbounds: &inbounds}, failingMutationSave)

	_, err := mutation.CreateInbound(Inbound{Name: "new", Protocol: "naiveproxy", Transport: "tcp", Port: 8443, Enabled: true})
	if !errors.Is(err, errMutationSaveFailed) {
		t.Fatalf("CreateInbound error = %v", err)
	}
	if !reflect.DeepEqual(inbounds, before) {
		t.Fatalf("inbounds changed after failed save: got=%+v want=%+v", inbounds, before)
	}
}

func TestMutationRollsBackRoutingRulesWhenSaveFails(t *testing.T) {
	before := []RoutingRule{{Name: "private", Match: "geoip:private", Outbound: "direct", Enabled: true}}
	rules := append([]RoutingRule(nil), before...)
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Rules: &rules}, failingMutationSave)

	_, err := mutation.UpdateRoutingRule("private", RoutingRule{Match: "geoip:private", Outbound: "block", Enabled: true})
	if !errors.Is(err, errMutationSaveFailed) {
		t.Fatalf("UpdateRoutingRule error = %v", err)
	}
	if !reflect.DeepEqual(rules, before) {
		t.Fatalf("routing rules changed after failed save: got=%+v want=%+v", rules, before)
	}
}

func TestMutationRollsBackWarpAndRulesWhenSaveFails(t *testing.T) {
	beforeWarp := WarpConfig{Enabled: false, Endpoint: "engage.cloudflareclient.com:2408", PrivateKey: "private-secret"}
	beforeRules := []RoutingRule{{Name: "private", Match: "geoip:private", Outbound: "direct", Enabled: true}}
	warp := beforeWarp
	rules := append([]RoutingRule(nil), beforeRules...)
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Warp: &warp, Rules: &rules}, failingMutationSave)

	_, err := mutation.UpdateWarp(WarpConfig{Enabled: true, PrivateKey: "[REDACTED]"})
	if !errors.Is(err, errMutationSaveFailed) {
		t.Fatalf("UpdateWarp error = %v", err)
	}
	if !reflect.DeepEqual(warp, beforeWarp) {
		t.Fatalf("warp changed after failed save: got=%+v want=%+v", warp, beforeWarp)
	}
	if !reflect.DeepEqual(rules, beforeRules) {
		t.Fatalf("warp routing rules changed after failed save: got=%+v want=%+v", rules, beforeRules)
	}
}

func TestMutationRollsBackUserOperationsWhenSaveFails(t *testing.T) {
	baseUsers := []model.User{
		{Username: "admin", PasswordHash: "admin-hash", Role: "admin", Locale: "en"},
		{Username: "viewer", PasswordHash: "viewer-hash", Role: "viewer", Locale: "ru"},
	}

	tests := []struct {
		name   string
		mutate func(Mutation) error
	}{
		{
			name: "create",
			mutate: func(m Mutation) error {
				_, err := m.CreateUser(model.User{Username: "second", PasswordHash: "second-hash", Role: "viewer", Locale: "en"})
				return err
			},
		},
		{
			name: "update",
			mutate: func(m Mutation) error {
				_, err := m.UpdateUser("viewer", model.User{PasswordHash: "changed-hash", Role: "admin", Locale: "en"})
				return err
			},
		},
		{
			name: "delete",
			mutate: func(m Mutation) error {
				return m.DeleteUser("viewer")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			users := cloneUsers(baseUsers)
			before := cloneUsers(users)
			mutation := NewManagementStateMutation(ManagementStateMutationTarget{Users: &users}, failingMutationSave)

			err := tc.mutate(mutation)
			if !errors.Is(err, errMutationSaveFailed) {
				t.Fatalf("mutation error = %v", err)
			}
			if !reflect.DeepEqual(users, before) {
				t.Fatalf("users changed after failed save: got=%+v want=%+v", users, before)
			}
		})
	}
}
