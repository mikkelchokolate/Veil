package api

import "testing"

func TestServiceCommandPolicyAllowsOnlyManagedReloadAndHealthServices(t *testing.T) {
	policy := ServiceCommandPolicy{}
	if !policy.AllowsReload([]string{"systemctl", "reload", "veil-naive.service"}) {
		t.Fatalf("expected veil-naive reload to be allowed")
	}
	if policy.AllowsReload([]string{"systemctl", "restart", "veil-naive.service"}) {
		t.Fatalf("restart must not be allowed")
	}
	if policy.AllowsHealth("ssh.service") {
		t.Fatalf("ssh.service health must not be allowed")
	}
}
