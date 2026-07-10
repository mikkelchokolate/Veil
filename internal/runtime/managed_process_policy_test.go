package runtime

import "testing"

func TestManagedProcessPolicyForRecognizesSuppliedNames(t *testing.T) {
	policy := NewManagedProcessPolicyFor([]string{"caddy", "hysteria2", "sing-box", "veil", "mieru"})
	for _, name := range []string{"caddy", "hysteria2", "sing-box", "veil", "mieru"} {
		if !policy.IsManaged(name) {
			t.Fatalf("%s should be managed", name)
		}
	}
	if policy.IsManaged("nginx") {
		t.Fatal("nginx should not be managed")
	}
}
