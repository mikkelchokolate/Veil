package runtime

import "testing"

func TestIsManagedProcessRecognizesManagedNames(t *testing.T) {
	for _, name := range []string{"caddy", "hysteria2", "sing-box", "veil", "mieru"} {
		if !isManagedProcess(name) {
			t.Fatalf("%s should be managed", name)
		}
	}
	if isManagedProcess("nginx") {
		t.Fatal("nginx should not be managed")
	}
}
