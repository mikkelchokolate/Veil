package generatedconfig

import (
	"errors"
	"testing"
)

func TestSetBuilderPropagatesRegistryRenderError(t *testing.T) {
	builder := NewSetBuilder(SetInput{
		ApplyRoot: "/etc/veil",
		Registry: NewProtocolRegistry([]Protocol{{Protocol: "mieru", Render: func(input ProtocolRenderInput) ([]GeneratedConfigArtifact, bool, error) {
			return nil, false, errors.New("registry render failed")
		}}}),
		Inbounds: []Inbound{{Name: "mieru", Protocol: "mieru", Enabled: true}},
	})
	_, err := builder.Build()
	if err == nil || err.Error() != "registry render failed" {
		t.Fatalf("expected registry render error, got %v", err)
	}
}

func TestSetBuilderPropagatesPanelAccessError(t *testing.T) {
	paths := NewPaths("/etc/veil")
	builder := NewSetBuilder(SetInput{
		ApplyRoot: "/etc/veil",
		Registry:  NewProtocolRegistry(nil),
		PanelAccess: func(Paths) (GeneratedConfigArtifact, bool, error) {
			return GeneratedConfigArtifact{}, false, errors.New("panel render failed")
		},
	})
	_, err := builder.Build()
	if err == nil || err.Error() != "panel render failed" {
		t.Fatalf("expected panel access error, got %v", err)
	}
	_ = paths
}

func TestSetBuilderPropagatesWarpError(t *testing.T) {
	paths := NewPaths("/etc/veil")
	builder := NewSetBuilder(SetInput{
		ApplyRoot: "/etc/veil",
		Registry:  NewProtocolRegistry(nil),
		Warp: func(Paths) (GeneratedConfigArtifact, bool, error) {
			return GeneratedConfigArtifact{}, false, errors.New("warp render failed")
		},
	})
	_, err := builder.Build()
	if err == nil || err.Error() != "warp render failed" {
		t.Fatalf("expected warp error, got %v", err)
	}
	_ = paths
}

func TestSetBuilderSkipsPanelAndWarpWhenNotProvided(t *testing.T) {
	builder := NewSetBuilder(SetInput{
		ApplyRoot: "/etc/veil",
		Registry:  NewProtocolRegistry(nil),
		Settings:  Settings{},
	})
	configs, err := builder.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(configs) != 0 {
		t.Fatalf("expected empty configs, got %+v", configs)
	}
}
