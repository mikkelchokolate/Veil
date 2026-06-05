package managementstate

import (
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
