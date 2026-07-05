package api

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestServiceCommandPolicyAllowsOnlyManagedPromotedActionsAndHealthServices(t *testing.T) {
	policy := service.NewCommandPolicy(NewManagedRuntimeCatalogFor([]Inbound{
		{Name: "naive", Protocol: "naiveproxy", Enabled: true},
		{Name: "mieru", Protocol: "mieru", Enabled: true},
	}, WarpConfig{}))
	if !policy.AllowsAction([]string{"systemctl", "reload", "veil-caddy.service"}) {
		t.Fatalf("expected veil-caddy reload to be allowed")
	}
	if !policy.AllowsAction([]string{"systemctl", "restart", "veil-mieru.service"}) {
		t.Fatalf("expected veil-mieru restart to be allowed")
	}
	if policy.AllowsAction([]string{"systemctl", "restart", "veil-caddy.service"}) {
		t.Fatalf("restart must not be allowed for single Caddy service")
	}
	if policy.AllowsAction([]string{"systemctl", "reload", "veil-mieru.service"}) {
		t.Fatalf("reload must not be allowed for Mieru")
	}
	if policy.AllowsHealth("ssh.service") {
		t.Fatalf("ssh.service health must not be allowed")
	}
}
