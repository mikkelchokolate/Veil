package repair

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepairPlanWarnsWhenTheBinaryCannotBeResolved locks the #1437 review
// follow-up: the warning must be emitted where every plan shape passes —
// including a host with no readable panel state, which never reaches the panel
// material and used to render the packaged binary silently.
func TestRepairPlanWarnsWhenTheBinaryCannotBeResolved(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state bool
	}{
		{name: "with panel state", state: true},
		{name: "without panel state", state: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{Profile: "ru-recommended", EtcDir: t.TempDir(), VarDir: t.TempDir(), SystemdDir: t.TempDir()}
			if tc.state {
				opts, _, _ = seedRotatedStateKey(t, true)
			}
			var warnings []string
			_, err := BuildPlanFromOptions(opts, PlanDependencies{
				Secret:     func(label string) string { return "repair-" + label },
				Executable: func() (string, error) { return "", errors.New("no /proc/self/exe") },
				LookPath:   testLookPath,
				Notify:     func(msg string) { warnings = append(warnings, msg) },
			})
			if err != nil {
				t.Fatalf("BuildPlanFromOptions: %v", err)
			}
			joined := strings.Join(warnings, "\n")
			for _, want := range []string{"could not resolve the running veil binary path", "no /proc/self/exe", "/usr/local/bin/veil", "Verify the rendered"} {
				if !strings.Contains(joined, want) {
					t.Fatalf("warnings %v do not mention %q", warnings, want)
				}
			}
		})
	}
}

// TestRepairPlanStaysQuietWhenTheBinaryResolves keeps the normal path quiet.
func TestRepairPlanStaysQuietWhenTheBinaryResolves(t *testing.T) {
	opts := Options{Profile: "ru-recommended", EtcDir: t.TempDir(), VarDir: t.TempDir(), SystemdDir: t.TempDir()}
	var warnings []string
	if _, err := BuildPlanFromOptions(opts, PlanDependencies{
		Secret:     func(label string) string { return "repair-" + label },
		Executable: func() (string, error) { return filepath.Join(t.TempDir(), "veil"), nil },
		LookPath:   testLookPath,
		Notify:     func(msg string) { warnings = append(warnings, msg) },
	}); err != nil {
		t.Fatalf("BuildPlanFromOptions: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
}
