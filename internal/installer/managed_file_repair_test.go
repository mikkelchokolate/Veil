package installer

import (
	"path/filepath"
	"testing"
)

func TestManagedFileRepairPlansMissingManagedFiles(t *testing.T) {
	paths := ApplyPaths{EtcDir: filepath.Join(t.TempDir(), "etc"), VarDir: filepath.Join(t.TempDir(), "var")}
	profile := RURecommendedProfile{Domain: "vpn.example.com", InstallNaive: true, Caddyfile: "caddy", Stack: StackNaive}
	plan, err := NewManagedFileRepair(profile, paths).Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if !plan.HasChanges() {
		t.Fatalf("expected missing files repair plan")
	}
}
