package api

import "testing"

func TestManagedProcessPolicyRecognizesVeilManagedProcesses(t *testing.T) {
	policy := NewManagedProcessPolicy()
	for _, name := range []string{"caddy", "hysteria2", "sing-box", "veil"} {
		if !policy.IsManaged(name) {
			t.Fatalf("%s should be managed", name)
		}
	}
	if policy.IsManaged("nginx") {
		t.Fatal("nginx should not be managed")
	}
}
