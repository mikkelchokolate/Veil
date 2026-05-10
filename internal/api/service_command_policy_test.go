package api

import (
	"testing"

	"github.com/veil-panel/veil/internal/service"
)

func TestServiceCommandPolicyAllowsOnlyManagedPromotedActionsAndHealthServices(t *testing.T) {
	policy := service.NewCommandPolicy(NewManagedRuntimeCatalog())
	if !policy.AllowsAction([]string{"systemctl", "reload", "veil-naive.service"}) {
		t.Fatalf("expected veil-naive reload to be allowed")
	}
	if !policy.AllowsAction([]string{"systemctl", "restart", "veil-mieru.service"}) {
		t.Fatalf("expected veil-mieru restart to be allowed")
	}
	if policy.AllowsAction([]string{"systemctl", "restart", "veil-naive.service"}) {
		t.Fatalf("restart must not be allowed for Naive")
	}
	if policy.AllowsAction([]string{"systemctl", "reload", "veil-mieru.service"}) {
		t.Fatalf("reload must not be allowed for Mieru")
	}
	if policy.AllowsHealth("ssh.service") {
		t.Fatalf("ssh.service health must not be allowed")
	}
}
