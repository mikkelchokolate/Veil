package cli

import (
	"errors"
	"testing"
)

func TestDoctorReadinessDoesNotRequireProtocolRuntimesForPanelOnlyInstall(t *testing.T) {
	oldLookPath := commandLookPath
	commandLookPath = func(name string) (string, error) {
		if name == "systemctl" {
			return "/bin/systemctl", nil
		}
		return "", errors.New("missing " + name)
	}
	t.Cleanup(func() { commandLookPath = oldLookPath })

	summary := NewDoctorReadiness("test-version").Summary()
	if !summary.Ready {
		t.Fatalf("Panel-only doctor should be ready without protocol runtimes: %+v", summary)
	}
	for _, command := range summary.Commands {
		if command.Name != "systemctl" && !command.Optional {
			t.Fatalf("protocol/runtime command should be optional: %+v", command)
		}
	}
}

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
