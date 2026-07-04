package managementstate

import (
	"errors"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestManagementStateMutationOwnsSettingsInboundAndWarpMutations(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", NaivePassword: "secret"}
	inbounds := []Inbound{}
	warp := WarpConfig{PrivateKey: "private-secret"}
	saves := 0
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Settings: &settings, Inbounds: &inbounds, Warp: &warp}, func() error {
		saves++
		return nil
	})

	updatedSettings, err := mutation.UpdateSettings(Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", NaivePassword: "[REDACTED]"})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if settings.NaivePassword != "secret" || updatedSettings.NaivePassword != "[REDACTED]" {
		t.Fatalf("settings credential policy not preserved: stored=%+v response=%+v", settings, updatedSettings)
	}

	created, err := mutation.CreateInbound(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true})
	if err != nil {
		t.Fatalf("CreateInbound: %v", err)
	}
	if created.Password == "" || len(inbounds) != 1 || inbounds[0].Name != "naive" {
		t.Fatalf("inbound mutation not applied: created=%+v inbounds=%+v", created, inbounds)
	}

	updatedWarp, err := mutation.UpdateWarp(WarpConfig{Enabled: true, PrivateKey: "[REDACTED]"})
	if err != nil {
		t.Fatalf("UpdateWarp: %v", err)
	}
	if warp.PrivateKey != "private-secret" || updatedWarp.PrivateKey != "[REDACTED]" || warp.Endpoint == "" {
		t.Fatalf("warp credential/default policy not applied: stored=%+v response=%+v", warp, updatedWarp)
	}

	if saves != 3 {
		t.Fatalf("saves = %d, want 3", saves)
	}
}

func TestManagementStateMutationDoesNotSaveOnInboundMutationError(t *testing.T) {
	inbounds := []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443}}
	saves := 0
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Inbounds: &inbounds}, func() error {
		saves++
		return nil
	})

	_, err := mutation.CreateInbound(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 9443})
	if err == nil {
		t.Fatalf("expected duplicate name error")
	}
	if saves != 0 {
		t.Fatalf("saves = %d, want 0", saves)
	}
}

func TestManagementStateMutationOwnsRoutingRuleMutations(t *testing.T) {
	rules := []RoutingRule{}
	saves := 0
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Rules: &rules}, func() error {
		saves++
		return nil
	})

	created, err := mutation.CreateRoutingRule(RoutingRule{Name: "non-ru", Match: "geosite:geolocation-!ru", Outbound: "warp", Enabled: true})
	if err != nil {
		t.Fatalf("CreateRoutingRule: %v", err)
	}
	if created.Name != "non-ru" || len(rules) != 1 {
		t.Fatalf("unexpected create result: created=%+v rules=%+v", created, rules)
	}
	if saves != 1 {
		t.Fatalf("saves = %d, want 1", saves)
	}

	_, err = mutation.CreateRoutingRule(RoutingRule{Name: "non-ru", Match: "geoip:ru", Outbound: "direct"})
	if err == nil {
		t.Fatalf("expected duplicate name error")
	}
	if saves != 1 {
		t.Fatalf("saves = %d, want still 1", saves)
	}
}

func TestManagementStateMutationPreservesPanelCaddyAccessFieldsWhenFormOmitsThem(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", PanelAccess: "caddy", WebBasePath: "/panel-secret/", Mode: "server"}
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Settings: &settings}, func() error { return nil })

	_, err := mutation.UpdateSettings(Settings{PanelListen: "127.0.0.1:2096", Mode: "server", Domain: "panel.example.com", Email: "admin@example.com"})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if settings.PanelAccess != "caddy" || settings.WebBasePath != "/panel-secret/" {
		t.Fatalf("Panel Caddy access fields not preserved: %+v", settings)
	}
}

func TestManagementStateMutationDoesNotSaveOnInvalidMutation(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}
	saves := 0
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Settings: &settings}, func() error {
		saves++
		return nil
	})

	_, err := mutation.UpdateSettings(Settings{PanelListen: "bad-listen", Mode: "dev"})
	if err == nil {
		t.Fatal("expected invalid settings error")
	}
	if saves != 0 {
		t.Fatalf("saves = %d, want 0", saves)
	}
}

func TestManagementStateMutationValidatesAndPreservesUserLocale(t *testing.T) {
	users := []model.User{{
		Username:     "viewer",
		PasswordHash: "hash",
		Role:         "viewer",
		Locale:       "ru",
	}}
	mutation := NewManagementStateMutation(ManagementStateMutationTarget{Users: &users}, func() error { return nil })

	updated, err := mutation.UpdateUser("viewer", model.User{PasswordHash: "hash-2", Role: "viewer"})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if updated.Locale != "ru" || users[0].Locale != "ru" {
		t.Fatalf("locale was not preserved: updated=%+v stored=%+v", updated, users[0])
	}

	if _, err := mutation.CreateUser(model.User{
		Username:     "bad-locale",
		PasswordHash: "hash",
		Role:         "viewer",
		Locale:       "de",
	}); err == nil {
		t.Fatal("expected unsupported locale error")
	}
}

func TestManagementStateCodecPersistsUserLocale(t *testing.T) {
	codec := NewManagementStateCodec()
	body, err := codec.Encode(model.ManagementSnapshot{
		Users: []model.User{{
			Username:     "admin",
			PasswordHash: "hash",
			Role:         "admin",
			Locale:       "ru",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := codec.Decode(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Users) != 1 || snapshot.Users[0].Locale != "ru" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestNewMutationUsesNopSaveWhenNil(t *testing.T) {
	m := NewMutation(MutationTarget{}, nil)
	if err := m.save(); err != nil {
		t.Fatalf("nil save should be nop: %v", err)
	}
}

func TestMutationNilTargetsReturnZeroValues(t *testing.T) {
	m := NewManagementStateMutation(ManagementStateMutationTarget{}, nil)
	if s := m.Settings(); s.PanelListen != "" {
		t.Fatalf("Settings() on nil target = %+v", s)
	}
	if in := m.Inbounds(); in != nil {
		t.Fatalf("Inbounds() on nil target = %+v", in)
	}
	if _, ok := m.Inbound("x"); ok {
		t.Fatal("Inbound() on nil target should return false")
	}
	if r := m.RoutingRules(); r != nil {
		t.Fatalf("RoutingRules() on nil target = %+v", r)
	}
	if _, ok := m.RoutingRule("x"); ok {
		t.Fatal("RoutingRule() on nil target should return false")
	}
	if w := m.Warp(); w.Endpoint != "" {
		t.Fatalf("Warp() on nil target = %+v", w)
	}
	if u := m.Users(); u != nil {
		t.Fatalf("Users() on nil target = %+v", u)
	}
}

func TestMutationUpdateAndDeleteInbound(t *testing.T) {
	inbounds := []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443}}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Inbounds: &inbounds}, func() error { return nil })

	updated, err := m.UpdateInbound("naive", Inbound{Name: "ignored", Protocol: "naiveproxy", Transport: "tcp", Port: 8443})
	if err != nil {
		t.Fatalf("UpdateInbound: %v", err)
	}
	if updated.Port != 8443 || updated.Name != "naive" || len(inbounds) != 1 || inbounds[0].Port != 8443 {
		t.Fatalf("update not applied: updated=%+v inbounds=%+v", updated, inbounds)
	}

	if _, err := m.UpdateInbound("missing", Inbound{Protocol: "naiveproxy", Transport: "tcp", Port: 8443}); err == nil {
		t.Fatal("expected not found error")
	}

	if err := m.DeleteInbound("naive"); err != nil {
		t.Fatalf("DeleteInbound: %v", err)
	}
	if len(inbounds) != 0 {
		t.Fatalf("inbounds = %+v", inbounds)
	}
	if err := m.DeleteInbound("missing"); err == nil {
		t.Fatal("expected not found error on delete")
	}
}

func TestMutationUpdateAndDeleteRoutingRule(t *testing.T) {
	rules := []RoutingRule{{Name: "direct", Match: "geoip:private", Outbound: "direct", Enabled: true}}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Rules: &rules}, func() error { return nil })

	if _, ok := m.RoutingRule("missing"); ok {
		t.Fatal("expected RoutingRule to return false for missing rule")
	}
	got, ok := m.RoutingRule("direct")
	if !ok || got.Name != "direct" {
		t.Fatalf("RoutingRule = %+v ok=%v", got, ok)
	}

	updated, err := m.UpdateRoutingRule("direct", RoutingRule{Match: "geoip:cn", Outbound: "block"})
	if err != nil {
		t.Fatalf("UpdateRoutingRule: %v", err)
	}
	if updated.Name != "direct" || updated.Outbound != "block" || rules[0].Outbound != "block" {
		t.Fatalf("update not applied: updated=%+v rules=%+v", updated, rules)
	}

	if _, err := m.UpdateRoutingRule("missing", RoutingRule{Match: "x", Outbound: "y"}); err == nil {
		t.Fatal("expected not found error")
	}
	if _, err := m.UpdateRoutingRule("direct", RoutingRule{Match: "", Outbound: "y"}); err == nil {
		t.Fatal("expected invalid rule error")
	}

	if err := m.DeleteRoutingRule("direct"); err != nil {
		t.Fatalf("DeleteRoutingRule: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("rules = %+v", rules)
	}
	if err := m.DeleteRoutingRule("missing"); err == nil {
		t.Fatal("expected not found error on delete")
	}
}

func TestMutationCreateRoutingRuleRejectsInvalidAndDuplicate(t *testing.T) {
	rules := []RoutingRule{{Name: "direct", Match: "geoip:private", Outbound: "direct"}}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Rules: &rules}, func() error { return nil })

	if _, err := m.CreateRoutingRule(RoutingRule{Name: "direct", Match: "x", Outbound: "y"}); err == nil {
		t.Fatal("expected duplicate name error")
	}
	if _, err := m.CreateRoutingRule(RoutingRule{Name: "invalid", Match: "", Outbound: "y"}); err == nil {
		t.Fatal("expected invalid rule error")
	}
}

func TestMutationUpdateWarpAddsAndRemovesRoutingRule(t *testing.T) {
	rules := []RoutingRule{{Name: "direct", Match: "geoip:private", Outbound: "direct", Enabled: true}}
	warp := WarpConfig{}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Rules: &rules, Warp: &warp}, func() error { return nil })

	updated, err := m.UpdateWarp(WarpConfig{Enabled: true})
	if err != nil {
		t.Fatalf("UpdateWarp: %v", err)
	}
	if !updated.Enabled || updated.Endpoint == "" {
		t.Fatalf("warp defaults not set: %+v", updated)
	}
	if len(rules) != 2 || rules[0].Outbound != "warp" {
		t.Fatalf("warp routing rule not added: %+v", rules)
	}

	updated, err = m.UpdateWarp(WarpConfig{Enabled: false})
	if err != nil {
		t.Fatalf("UpdateWarp disable: %v", err)
	}
	if updated.Enabled {
		t.Fatal("expected warp disabled")
	}
	if len(rules) != 1 || rules[0].Outbound == "warp" {
		t.Fatalf("warp routing rule not removed: %+v", rules)
	}
}

func TestMutationUpdateWarpKeepsExistingWarpRule(t *testing.T) {
	rules := []RoutingRule{{Name: "custom-warp", Match: "geoip:ru", Outbound: "warp", Enabled: true}}
	warp := WarpConfig{}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Rules: &rules, Warp: &warp}, func() error { return nil })

	if _, err := m.UpdateWarp(WarpConfig{Enabled: true}); err != nil {
		t.Fatalf("UpdateWarp: %v", err)
	}
	if len(rules) != 1 || rules[0].Name != "custom-warp" {
		t.Fatalf("existing warp rule should be preserved: %+v", rules)
	}
}

func TestMutationWarpRedactsAndPreservesSecrets(t *testing.T) {
	warp := WarpConfig{PrivateKey: "secret-key", LicenseKey: "secret-license"}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Warp: &warp}, func() error { return nil })

	view := m.Warp()
	if view.PrivateKey == "secret-key" || view.LicenseKey == "secret-license" {
		t.Fatalf("Warp() did not redact: %+v", view)
	}

	updated, err := m.UpdateWarp(WarpConfig{Enabled: true, PrivateKey: "[REDACTED]", LicenseKey: "[REDACTED]"})
	if err != nil {
		t.Fatalf("UpdateWarp: %v", err)
	}
	if warp.PrivateKey != "secret-key" || warp.LicenseKey != "secret-license" {
		t.Fatalf("secrets not preserved: %+v", warp)
	}
	if updated.PrivateKey != "[REDACTED]" || updated.LicenseKey != "[REDACTED]" {
		t.Fatalf("response not redacted: %+v", updated)
	}
}

func TestMutationCreateUserValidations(t *testing.T) {
	users := []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin", Locale: "en"}}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Users: &users}, func() error { return nil })

	if _, err := m.CreateUser(model.User{Username: "", PasswordHash: "hash", Role: "admin"}); err == nil {
		t.Fatal("expected invalid user data error for empty username")
	}
	if _, err := m.CreateUser(model.User{Username: "hacker", PasswordHash: "hash", Role: "superuser"}); err == nil {
		t.Fatal("expected invalid role error")
	}
	if _, err := m.CreateUser(model.User{Username: "admin", PasswordHash: "hash", Role: "admin"}); err == nil {
		t.Fatal("expected duplicate user error")
	}
	if _, err := m.CreateUser(model.User{Username: "viewer", PasswordHash: "hash", Role: "viewer", Locale: "de"}); err == nil {
		t.Fatal("expected invalid locale error")
	}

	created, err := m.CreateUser(model.User{Username: "viewer", PasswordHash: "hash", Role: "viewer", Locale: "ru"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.Username != "viewer" || len(users) != 2 {
		t.Fatalf("user not created: %+v users=%+v", created, users)
	}
}

func TestMutationUpdateUserRejectsInvalidRoleAndLocale(t *testing.T) {
	users := []model.User{{Username: "viewer", PasswordHash: "hash", Role: "viewer", Locale: "ru"}}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Users: &users}, func() error { return nil })

	if _, err := m.UpdateUser("viewer", model.User{PasswordHash: "hash", Role: "owner"}); err == nil {
		t.Fatal("expected invalid role error")
	}
	if _, err := m.UpdateUser("viewer", model.User{PasswordHash: "hash", Role: "viewer", Locale: "de"}); err == nil {
		t.Fatal("expected invalid locale error")
	}
	if _, err := m.UpdateUser("missing", model.User{PasswordHash: "hash", Role: "viewer"}); err == nil {
		t.Fatal("expected user not found error")
	}
}

func TestMutationDeleteUser(t *testing.T) {
	users := []model.User{
		{Username: "admin", PasswordHash: "hash", Role: "admin"},
		{Username: "viewer", PasswordHash: "hash", Role: "viewer"},
	}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Users: &users}, func() error { return nil })

	if err := m.DeleteUser("viewer"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if len(users) != 1 || users[0].Username != "admin" {
		t.Fatalf("users = %+v", users)
	}
	if err := m.DeleteUser("missing"); err == nil {
		t.Fatal("expected user not found error")
	}
}

func TestMutationSaveErrorPropagates(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}
	inbounds := []Inbound{}
	rules := []RoutingRule{}
	users := []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}}
	warp := WarpConfig{}
	saveErr := errors.New("save failed")
	m := NewManagementStateMutation(ManagementStateMutationTarget{
		Settings: &settings,
		Inbounds: &inbounds,
		Rules:    &rules,
		Warp:     &warp,
		Users:    &users,
	}, func() error { return saveErr })

	if _, err := m.UpdateSettings(Settings{PanelListen: "127.0.0.1:2096", Mode: "dev"}); err != saveErr {
		t.Fatalf("UpdateSettings err = %v", err)
	}
	m = NewManagementStateMutation(ManagementStateMutationTarget{Inbounds: &inbounds}, func() error { return saveErr })
	if _, err := m.CreateInbound(Inbound{Name: "a", Protocol: "mieru", Transport: "tcp", Port: 8443}); err != saveErr {
		t.Fatalf("CreateInbound err = %v", err)
	}
	m = NewManagementStateMutation(ManagementStateMutationTarget{Rules: &rules}, func() error { return saveErr })
	if _, err := m.CreateRoutingRule(RoutingRule{Name: "r", Match: "x", Outbound: "y"}); err != saveErr {
		t.Fatalf("CreateRoutingRule err = %v", err)
	}
	m = NewManagementStateMutation(ManagementStateMutationTarget{Warp: &warp}, func() error { return saveErr })
	if _, err := m.UpdateWarp(WarpConfig{Enabled: true}); err != saveErr {
		t.Fatalf("UpdateWarp err = %v", err)
	}
	m = NewManagementStateMutation(ManagementStateMutationTarget{Users: &users}, func() error { return saveErr })
	if _, err := m.CreateUser(model.User{Username: "new", PasswordHash: "hash", Role: "viewer"}); err != saveErr {
		t.Fatalf("CreateUser err = %v", err)
	}
}

func TestMutationSettingsGetterRedactsSecrets(t *testing.T) {
	settings := Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", NaivePassword: "secret"}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Settings: &settings}, func() error { return nil })
	view := m.Settings()
	if view.NaivePassword == "secret" {
		t.Fatalf("Settings() did not redact: %+v", view)
	}
}

func TestMutationInboundGetter(t *testing.T) {
	inbounds := []Inbound{{Name: "a", Protocol: "mieru", Transport: "tcp", Port: 443}}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Inbounds: &inbounds}, func() error { return nil })
	if _, ok := m.Inbound("missing"); ok {
		t.Fatal("expected Inbound to return false for missing")
	}
	got, ok := m.Inbound("a")
	if !ok || got.Name != "a" {
		t.Fatalf("Inbound = %+v ok=%v", got, ok)
	}
}

func TestMutationUpdateRoutingRuleSaveError(t *testing.T) {
	rules := []RoutingRule{{Name: "direct", Match: "geoip:private", Outbound: "direct"}}
	saveErr := errors.New("save failed")
	m := NewManagementStateMutation(ManagementStateMutationTarget{Rules: &rules}, func() error { return saveErr })
	if _, err := m.UpdateRoutingRule("direct", RoutingRule{Match: "geoip:cn", Outbound: "block"}); err != saveErr {
		t.Fatalf("UpdateRoutingRule err = %v", err)
	}
}

func TestMutationUpdateUserWithExplicitLocale(t *testing.T) {
	users := []model.User{{Username: "viewer", PasswordHash: "hash", Role: "viewer", Locale: "ru"}}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Users: &users}, func() error { return nil })
	updated, err := m.UpdateUser("viewer", model.User{PasswordHash: "hash", Role: "viewer", Locale: "en"})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if updated.Locale != "en" || users[0].Locale != "en" {
		t.Fatalf("locale not updated: updated=%+v stored=%+v", updated, users[0])
	}
}

func TestMutationUsersReturnsClonedSlice(t *testing.T) {
	users := []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}}
	m := NewManagementStateMutation(ManagementStateMutationTarget{Users: &users}, func() error { return nil })
	got := m.Users()
	if len(got) != 1 || got[0].Username != "admin" {
		t.Fatalf("Users = %+v", got)
	}
	got[0].Username = "mutated"
	if users[0].Username != "admin" {
		t.Fatalf("Users() returned mutable reference")
	}
}

func TestMutationUpdateInboundSaveError(t *testing.T) {
	inbounds := []Inbound{{Name: "a", Protocol: "mieru", Transport: "tcp", Port: 443}}
	saveErr := errors.New("save failed")
	m := NewManagementStateMutation(ManagementStateMutationTarget{Inbounds: &inbounds}, func() error { return saveErr })
	if _, err := m.UpdateInbound("a", Inbound{Protocol: "mieru", Transport: "tcp", Port: 8443}); err != saveErr {
		t.Fatalf("UpdateInbound err = %v", err)
	}
}

func TestMutationUpdateUserSaveError(t *testing.T) {
	users := []model.User{{Username: "admin", PasswordHash: "hash", Role: "admin"}}
	saveErr := errors.New("save failed")
	m := NewManagementStateMutation(ManagementStateMutationTarget{Users: &users}, func() error { return saveErr })
	if _, err := m.UpdateUser("admin", model.User{PasswordHash: "hash", Role: "admin"}); err != saveErr {
		t.Fatalf("UpdateUser err = %v", err)
	}
}
