package cli

import "testing"

func TestDoctorReadinessBuildsSummary(t *testing.T) {
	oldLookPath := commandLookPath
	commandLookPath = func(name string) (string, error) { return "/bin/" + name, nil }
	t.Cleanup(func() { commandLookPath = oldLookPath })

	summary := NewDoctorReadiness("test-version").Summary()
	if summary.Version != "test-version" || !summary.Ready {
		t.Fatalf("summary = %+v", summary)
	}
	if len(summary.Commands) == 0 {
		t.Fatalf("expected command checks")
	}
}
