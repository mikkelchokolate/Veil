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
	if _, ok := policy.ManagedUnits["veil.service"]; !ok {
		t.Fatal("expected veil.service to be managed")
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
