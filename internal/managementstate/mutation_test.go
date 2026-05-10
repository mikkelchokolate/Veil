package managementstate

import "testing"

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
