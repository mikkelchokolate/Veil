package privileged

import (
	"path/filepath"
	"testing"
)

func TestDefaultPolicyProvidesManagedPathsAndUnits(t *testing.T) {
	policy := DefaultPolicy()
	if policy.StagingRoot == "" || policy.GeneratedRoot == "" || policy.StateRoot == "" {
		t.Fatal("expected default roots to be set")
	}
	for _, unit := range []string{"veil.service", "veil-mieru.service", "veil-warp.service", "veil-caddy@.service", "veil-hysteria2@.service", "veil-olcrtc@.service"} {
		if _, ok := policy.ManagedUnits[unit]; !ok {
			t.Fatalf("expected %s to be managed in default policy", unit)
		}
	}
	for _, prefix := range []string{"veil-caddy@", "veil-hysteria2@", "veil-olcrtc@"} {
		if !containsDefaultPolicyPrefix(policy.ManagedUnitPrefixes, prefix) {
			t.Fatalf("expected managed unit prefix %s in %+v", prefix, policy.ManagedUnitPrefixes)
		}
	}
	if _, ok := policy.Artifacts["mieru"]; !ok {
		t.Fatal("expected mieru artifact to be registered")
	}
	if _, ok := policy.UpdateArtifacts["veil-update"]; !ok {
		t.Fatal("expected veil-update artifact to be registered")
	}
	if filepath.IsAbs(DefaultSocketPath) != true {
		t.Fatal("expected default socket path to be absolute")
	}
}

func containsDefaultPolicyPrefix(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
