package installer

import "testing"

func TestInstallPlanDefaultsFillPlatformAndHysteriaVersion(t *testing.T) {
	input := NewInstallPlanDefaults(func() Platform { return Platform{OS: "linux", Arch: "amd64"} }).Apply(InstallPlanInput{})
	if input.Platform.OS != "linux" || input.Platform.Arch != "amd64" {
		t.Fatalf("platform = %+v", input.Platform)
	}
	if input.HysteriaVersion != "v2.6.0" {
		t.Fatalf("version = %q", input.HysteriaVersion)
	}
}

func TestInstallPlanDefaultsPreserveProvidedValues(t *testing.T) {
	input := NewInstallPlanDefaults(func() Platform { return Platform{OS: "linux", Arch: "amd64"} }).Apply(InstallPlanInput{Platform: Platform{OS: "linux", Arch: "arm64"}, HysteriaVersion: "v2.7.0"})
	if input.Platform.Arch != "arm64" || input.HysteriaVersion != "v2.7.0" {
		t.Fatalf("input = %+v", input)
	}
}
