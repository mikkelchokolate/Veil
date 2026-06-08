package api

import (
	"os"
	"testing"
)

// TestMain makes the systemd status reader hermetic by default: tests must not
// depend on whatever units happen to run on the host executing them. Tests that
// need specific statuses override serviceStatusReader and restore it.
func TestMain(m *testing.M) {
	serviceStatusReader = func(unit string) ServiceRuntimeStatus {
		return ServiceRuntimeStatus{Unit: unit, LoadState: "not-found", ActiveState: "inactive", SubState: "dead"}
	}
	os.Exit(m.Run())
}
