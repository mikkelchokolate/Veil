package privileged

import (
	"errors"
	"os"
	"os/user"
	"testing"
)

// stubRuntimeArtifactOwnership makes promotion/runtime-artifact ownership tests
// hermetic: the process reports root, group lookups resolve veil/veil-proxy,
// and chown/chmod are recorded no-ops. Tests that need to inspect or inject
// failures should stub the individual hooks themselves instead.
func stubRuntimeArtifactOwnership(t *testing.T) {
	t.Helper()
	oldEffectiveUID := effectiveUID
	oldLookupGroup := lookupGroup
	oldChownPath := chownPath
	oldChmodPath := chmodPath
	t.Cleanup(func() {
		effectiveUID = oldEffectiveUID
		lookupGroup = oldLookupGroup
		chownPath = oldChownPath
		chmodPath = oldChmodPath
	})
	effectiveUID = func() int { return 0 }
	lookupGroup = func(name string) (*user.Group, error) {
		switch name {
		case "veil":
			return &user.Group{Gid: "456"}, nil
		case "veil-proxy":
			return &user.Group{Gid: "457"}, nil
		default:
			return nil, errors.New("unknown test group " + name)
		}
	}
	chownPath = func(string, int, int) error { return nil }
	chmodPath = func(string, os.FileMode) error { return nil }
}
