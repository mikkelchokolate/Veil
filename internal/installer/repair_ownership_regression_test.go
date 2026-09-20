package installer

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

// Regression for #379: repaired files are rewritten via atomic temp+rename, so
// they land root:root with the plan mode. ApplyRepairPlan must re-apply the
// install ownership contract — panel TLS and generated configs end
// root:veil-proxy 0640 (dir 0750) so User=veil-proxy units and Caddy can read
// them again.
func TestApplyRepairPlanRestoresRuntimeSharedOwnership(t *testing.T) {
	oldUID, oldLookupG := effectiveUID, lookupGroup
	defer func() { effectiveUID, lookupGroup = oldUID, oldLookupG }()
	effectiveUID = func() int { return 0 }
	lookupGroup = func(name string) (*user.Group, error) {
		switch name {
		case "veil":
			return &user.Group{Gid: "100"}, nil
		case "veil-proxy":
			return &user.Group{Gid: "101"}, nil
		}
		return nil, fmt.Errorf("unknown group %s", name)
	}
	oldChown, oldChmod := chownPath, chmodPath
	defer func() { chownPath, chmodPath = oldChown, oldChmod }()
	chowns := map[string]int{}
	chmods := map[string]os.FileMode{}
	chownPath = func(path string, _, gid int) error {
		chowns[path] = gid
		return nil
	}
	chmodPath = func(path string, mode os.FileMode) error {
		chmods[path] = mode
		return nil
	}

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "panel", "tls.key")
	caddyPath := filepath.Join(dir, "generated", "caddy", "config.json")
	envPath := filepath.Join(dir, "veil.env")
	plan := RepairPlan{Actions: []RepairAction{
		{Path: keyPath, Reason: RepairReasonMissing, Content: "key", Mode: 0o600},
		{Path: caddyPath, Reason: RepairReasonMissing, Content: "{}", Mode: 0o600},
		{Path: envPath, Reason: RepairReasonMissing, Content: "VEIL_API_TOKEN=x\n", Mode: 0o600},
	}}

	result, err := ApplyRepairPlan(plan)
	if err != nil {
		t.Fatalf("apply repair: %v", err)
	}
	if len(result.WrittenFiles) != 3 {
		t.Fatalf("written files: %+v", result.WrittenFiles)
	}
	// Runtime-shared artifacts (panel TLS, generated configs) are grouped to
	// veil-proxy; the panel env stays in the veil group.
	for path, wantGID := range map[string]int{keyPath: 101, caddyPath: 101, envPath: 100} {
		if got := chowns[path]; got != wantGID {
			t.Fatalf("%s chown gid=%d, want %d", path, got, wantGID)
		}
		if got := chmods[path]; got != 0o640 {
			t.Fatalf("%s chmod=%#o, want 0640", path, got)
		}
	}
	for _, dir := range []string{filepath.Dir(keyPath), filepath.Dir(caddyPath)} {
		if got := chowns[dir]; got != 101 {
			t.Fatalf("%s chown gid=%d, want veil-proxy gid 101", dir, got)
		}
		if got := chmods[dir]; got != 0o750 {
			t.Fatalf("%s chmod=%#o, want 0750", dir, got)
		}
	}
}
