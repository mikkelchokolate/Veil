package privileged

import (
	"errors"
	"os"
	"os/user"
	"testing"
)

// stubRuntimeArtifactOwnership makes promotion/runtime-artifact ownership tests
// hermetic: the process reports root, account lookups resolve veil/veil-proxy,
// and chown/chmod are recorded no-ops. Tests that need to inspect or inject
// failures should stub the individual hooks themselves instead.
func stubRuntimeArtifactOwnership(t *testing.T) {
	t.Helper()
	oldEffectiveUID := effectiveUID
	oldLookupUser := lookupUser
	oldChownPath := chownPath
	oldChmodPath := chmodPath
	t.Cleanup(func() {
		effectiveUID = oldEffectiveUID
		lookupUser = oldLookupUser
		chownPath = oldChownPath
		chmodPath = oldChmodPath
	})
	effectiveUID = func() int { return 0 }
	lookupUser = func(name string) (*user.User, error) {
		switch name {
		case "veil":
			return &user.User{Uid: "123", Gid: "456"}, nil
		case "veil-proxy":
			return &user.User{Uid: "124", Gid: "457"}, nil
		default:
			return nil, errors.New("unknown test account " + name)
		}
	}
	chownPath = func(string, int, int) error { return nil }
	chmodPath = func(string, os.FileMode) error { return nil }
}
