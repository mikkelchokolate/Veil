package managementstate

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildSnapshotClonesMutableSlices(t *testing.T) {
	inbounds := []model.Inbound{{Name: "default", Profiles: []model.ClientProfile{{Name: "owner"}}}}
	rules := []model.RoutingRule{{Name: "private"}}
	snapshot := BuildSnapshot(SnapshotInput{Inbounds: inbounds, Rules: rules})
	inbounds[0].Name = "mutated"
	inbounds[0].Profiles[0].Name = "mutated-profile"
	rules[0].Name = "mutated-rule"
	if snapshot.Inbounds[0].Name != "default" || snapshot.Inbounds[0].Profiles[0].Name != "owner" || snapshot.Rules[0].Name != "private" {
		t.Fatalf("snapshot was not cloned deeply enough: %+v", snapshot)
	}
}

func TestApplySnapshotPreservesMissingOptionalSections(t *testing.T) {
	settings := model.Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "default.example"}
	inbounds := []model.Inbound{{Name: "default"}}
	rules := []model.RoutingRule{{Name: "default-rule"}}
	routingPreset := "default-preset"
	routingSource := model.RoutingSource{Repository: "default-repo"}
	warp := model.WarpConfig{Endpoint: "default-endpoint"}

	ApplySnapshot(SnapshotTarget{Settings: &settings, Inbounds: &inbounds, Rules: &rules, RoutingPreset: &routingPreset, RoutingSource: &routingSource, Warp: &warp}, model.ManagementSnapshot{Settings: model.Settings{PanelListen: "0.0.0.0:2096", Mode: "dev"}})

	if settings.PanelListen != "0.0.0.0:2096" || settings.Domain != "default.example" {
		t.Fatalf("settings = %+v", settings)
	}
	if inbounds[0].Name != "default" || rules[0].Name != "default-rule" || routingPreset != "default-preset" || routingSource.Repository != "default-repo" || warp.Endpoint != "default-endpoint" {
		t.Fatalf("optional sections were not preserved: inbounds=%+v rules=%+v preset=%q source=%+v warp=%+v", inbounds, rules, routingPreset, routingSource, warp)
	}
}

func TestBuildSnapshotClonesUsers(t *testing.T) {
	users := []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}}
	snapshot := BuildSnapshot(SnapshotInput{Users: users})
	users[0].Username = "mutated"
	if snapshot.Users[0].Username != "admin" {
		t.Fatalf("users not cloned: %+v", snapshot.Users)
	}
}

func TestApplySnapshotUpdatesAllSections(t *testing.T) {
	var setup model.SetupState
	var settings model.Settings
	var inbounds []model.Inbound
	var rules []model.RoutingRule
	var routingPreset string
	var routingSource model.RoutingSource
	var warp model.WarpConfig
	var users []model.User

	input := model.ManagementSnapshot{
		Setup:         model.SetupState{Completed: true, CompletedAt: "now"},
		Settings:      model.Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"},
		Inbounds:      []model.Inbound{{Name: "i", Profiles: []model.ClientProfile{{Name: "p"}}}},
		Rules:         []model.RoutingRule{{Name: "r"}},
		RoutingPreset: "preset",
		RoutingSource: model.RoutingSource{Repository: "repo", Files: []model.RoutingSourceFile{{Name: "f", URL: "u"}}},
		Warp:          model.WarpConfig{Enabled: true, Endpoint: "e"},
		Users:         []model.User{{Username: "u"}},
	}

	ApplySnapshot(SnapshotTarget{
		Setup:         &setup,
		Settings:      &settings,
		Inbounds:      &inbounds,
		Rules:         &rules,
		RoutingPreset: &routingPreset,
		RoutingSource: &routingSource,
		Warp:          &warp,
		Users:         &users,
	}, input)

	if !setup.Completed {
		t.Fatalf("setup not applied: %+v", setup)
	}
	if settings.PanelListen != "127.0.0.1:2096" {
		t.Fatalf("settings not applied: %+v", settings)
	}
	if len(inbounds) != 1 || inbounds[0].Name != "i" || inbounds[0].Profiles[0].Name != "p" {
		t.Fatalf("inbounds not applied: %+v", inbounds)
	}
	if len(rules) != 1 || rules[0].Name != "r" {
		t.Fatalf("rules not applied: %+v", rules)
	}
	if routingPreset != "preset" {
		t.Fatalf("routingPreset not applied: %q", routingPreset)
	}
	if routingSource.Repository != "repo" || len(routingSource.Files) != 1 {
		t.Fatalf("routingSource not applied: %+v", routingSource)
	}
	if !warp.Enabled || warp.Endpoint != "e" {
		t.Fatalf("warp not applied: %+v", warp)
	}
	if len(users) != 1 || users[0].Username != "u" {
		t.Fatalf("users not applied: %+v", users)
	}
}

func TestApplySnapshotIgnoresNilTargets(t *testing.T) {
	input := model.ManagementSnapshot{
		Settings: model.Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"},
		Inbounds: []model.Inbound{{Name: "i"}},
		Rules:    []model.RoutingRule{{Name: "r"}},
		Warp:     model.WarpConfig{Enabled: true, Endpoint: "e"},
		Users:    []model.User{{Username: "u"}},
	}
	ApplySnapshot(SnapshotTarget{}, input)
}

func TestApplySnapshotSkipsEmptyValues(t *testing.T) {
	var settings model.Settings
	var routingPreset string
	var routingSource model.RoutingSource
	var warp model.WarpConfig

	ApplySnapshot(SnapshotTarget{Settings: &settings, RoutingPreset: &routingPreset, RoutingSource: &routingSource, Warp: &warp}, model.ManagementSnapshot{})
	if settings.PanelListen != "" {
		t.Fatalf("settings updated unexpectedly: %+v", settings)
	}
	if routingPreset != "" {
		t.Fatalf("routingPreset updated unexpectedly: %q", routingPreset)
	}
}

func TestMergeSettingsDefaults(t *testing.T) {
	defaults := model.Settings{
		PanelListen: "127.0.0.1:2096",
		PanelAccess: "direct",
		WebBasePath: "/panel/",
		Mode:        "dev",
		Domain:      "default.example",
		Email:       "admin@default.example",
	}
	settings := model.Settings{Mode: "server"}
	merged := MergeSettingsDefaults(settings, defaults)
	if merged.PanelListen != defaults.PanelListen ||
		merged.PanelAccess != defaults.PanelAccess ||
		merged.WebBasePath != defaults.WebBasePath ||
		merged.Mode != "server" ||
		merged.Domain != defaults.Domain ||
		merged.Email != defaults.Email {
		t.Fatalf("merged = %+v", merged)
	}
}

func TestCloneUsersNil(t *testing.T) {
	if cloneUsers(nil) != nil {
		t.Fatal("cloneUsers(nil) should return nil")
	}
}

func TestCloneInboundsNil(t *testing.T) {
	if cloneInbounds(nil) != nil {
		t.Fatal("cloneInbounds(nil) should return nil")
	}
}
