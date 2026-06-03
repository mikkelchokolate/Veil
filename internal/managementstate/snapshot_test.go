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
