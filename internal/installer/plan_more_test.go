package installer

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

func TestCaddyPanelBuildHintUsesDefaultBinaryPath(t *testing.T) {
	hint := CaddyPanelBuildHint("")
	if hint.BinaryPath != "/usr/local/bin/caddy" {
		t.Fatalf("expected default binary path %q, got %q", "/usr/local/bin/caddy", hint.BinaryPath)
	}
	want := "requires standard Caddy at /usr/local/bin/caddy"
	if len(hint.Commands) != 1 || hint.Commands[0] != want {
		t.Fatalf("expected command %q, got %v", want, hint.Commands)
	}
}

func TestBuildInstallPlanUsesCurrentPlatformWhenEmpty(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Secret: func(label string) string { return "secret" }})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}

	plan, err := BuildInstallPlan(profile, InstallPlanInput{SystemdUnits: []string{"veil.service"}})
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}

	current := hostenv.CurrentPlatform()
	if plan.Platform.OS != current.OS {
		t.Fatalf("expected OS %q, got %q", current.OS, plan.Platform.OS)
	}
}

func TestBuildInstallPlanRejectsInvalidPlatform(t *testing.T) {
	profile, err := BuildRURecommendedProfile(RURecommendedInput{Secret: func(label string) string { return "secret" }})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}

	tests := []struct {
		name     string
		platform hostenv.Platform
		wantErr  string
	}{
		{
			name:     "unsupported os",
			platform: hostenv.Platform{OS: "windows", Arch: "amd64"},
			wantErr:  "unsupported os",
		},
		{
			name:     "unsupported arch",
			platform: hostenv.Platform{OS: "linux", Arch: "mips"},
			wantErr:  "unsupported arch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildInstallPlan(profile, InstallPlanInput{Platform: tt.platform})
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
