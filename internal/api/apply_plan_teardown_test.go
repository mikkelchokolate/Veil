package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// TestBuildApplyPlanPureTeardownListsMutations locks #1134: removing the last
// artifact a unit served must appear in the plan as remove/stop/disable and
// firewall operations — a pure teardown can never reduce to just "validate
// management state".
func TestBuildApplyPlanPureTeardownListsMutations(t *testing.T) {
	root := t.TempDir()
	liveRoot := filepath.Join(root, "live")
	wantsDir := filepath.Join(root, "wants")

	// Stale live WARP artifact: WARP was once applied, now disabled.
	warpLive := filepath.Join(liveRoot, "sing-box", "warp.json")
	if err := os.MkdirAll(filepath.Dir(warpLive), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(warpLive, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// An enabled template unit left behind by an earlier apply.
	if err := os.MkdirAll(wantsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wantsDir, "veil-hysteria2@stale.service"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	plan := BuildApplyPlan(ApplyPlanInput{
		ApplyRoot:       filepath.Join(root, "apply"),
		LiveRoot:        liveRoot,
		SystemdWantsDir: wantsDir,
		Settings:        Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "vpn.example.com", DefaultAcmeEmail: "admin@example.com"},
	})
	if !plan.Valid {
		t.Fatalf("teardown plan should be valid: %+v", plan)
	}

	for _, want := range []string{
		"remove " + filepath.ToSlash(warpLive),
		"stop veil-warp.service",
		"disable veil-warp.service",
		"stop veil-hysteria2@stale.service",
		"disable veil-hysteria2@stale.service",
		"reconcile firewall rules",
	} {
		if !containsString(plan.Actions, want) {
			t.Fatalf("pure teardown plan missing action %q: %v", want, plan.Actions)
		}
	}

	kinds := map[string]int{}
	for _, op := range plan.Operations {
		kinds[op.Type]++
	}
	for _, want := range []string{"remove_file", "stop_service", "disable_service", "reconcile_firewall"} {
		if kinds[want] == 0 {
			t.Fatalf("pure teardown plan missing %s operation: %+v", want, plan.Operations)
		}
	}
	var removeOp *model.ApplyOperation
	for i := range plan.Operations {
		if plan.Operations[i].Type == "remove_file" && filepath.ToSlash(plan.Operations[i].Destination) == filepath.ToSlash(warpLive) {
			removeOp = &plan.Operations[i]
		}
	}
	if removeOp == nil {
		t.Fatalf("no remove_file operation for the stale warp artifact: %+v", plan.Operations)
	}
}

// TestBuildApplyPlanListsEnableForDesiredServices locks the other half of
// #1134: desired non-veil units are (re-)enabled for boot persistence during
// the reload phase, and the plan must say so.
func TestBuildApplyPlanListsEnableForDesiredServices(t *testing.T) {
	root := t.TempDir()
	plan := BuildApplyPlan(ApplyPlanInput{
		ApplyRoot: filepath.Join(root, "apply"),
		LiveRoot:  filepath.Join(root, "live"),
		Settings:  Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "vpn.example.com", DefaultAcmeEmail: "admin@example.com"},
		Inbounds: []Inbound{
			{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 14460, Enabled: true, Password: "secret"},
		},
	})
	if !plan.Valid {
		t.Fatalf("plan should be valid: %+v", plan)
	}
	found := false
	for _, op := range plan.Operations {
		if op.Type == "enable_service" && op.Unit == "veil-hysteria2@hy2.service" {
			found = true
		}
	}
	if !found {
		t.Fatalf("plan missing enable_service for desired unit: %+v", plan.Operations)
	}
	if !containsString(plan.Actions, "reconcile firewall rules") {
		t.Fatalf("plan missing firewall reconcile action: %v", plan.Actions)
	}
}
