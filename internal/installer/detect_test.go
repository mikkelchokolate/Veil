package installer

import "testing"

func TestRandomHighPortIsInExpectedRange(t *testing.T) {
	port, err := RandomHighPort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port < 20000 || port > 50000 {
		t.Fatalf("expected port in 20000..50000, got %d", port)
	}
}
