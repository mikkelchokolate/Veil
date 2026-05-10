package installer

import (
	"testing"

	"github.com/veil-panel/veil/internal/hostenv"
)

func TestInstallPlanDefaultsFillPlatform(t *testing.T) {
	input := NewInstallPlanDefaults(func() hostenv.Platform { return hostenv.Platform{OS: "linux", Arch: "amd64"} }).Apply(InstallPlanInput{})
	if input.Platform.OS != "linux" || input.Platform.Arch != "amd64" {
		t.Fatalf("platform = %+v", input.Platform)
	}
}

func TestInstallPlanDefaultsPreserveProvidedPlatform(t *testing.T) {
	input := NewInstallPlanDefaults(func() hostenv.Platform { return hostenv.Platform{OS: "linux", Arch: "amd64"} }).Apply(InstallPlanInput{Platform: hostenv.Platform{OS: "linux", Arch: "arm64"}})
	if input.Platform.Arch != "arm64" {
		t.Fatalf("input = %+v", input)
	}
}
