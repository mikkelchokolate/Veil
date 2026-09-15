package installer

import (
	"fmt"
	"strings"
	"testing"
)

func TestSelectPanelPortUsesUserProvidedPort(t *testing.T) {
	port, random, err := SelectPanelPort(2096, func() (int, error) { return 31874, nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 2096 || random {
		t.Fatalf("expected user port 2096, got port=%d random=%v", port, random)
	}
}

func TestSelectPanelPortUsesRandomWhenZero(t *testing.T) {
	port, random, err := SelectPanelPort(0, func() (int, error) { return 31874, nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 31874 || !random {
		t.Fatalf("expected random port 31874, got port=%d random=%v", port, random)
	}
}

func TestSelectPanelPortRejectsInvalidPort(t *testing.T) {
	if _, _, err := SelectPanelPort(70000, RandomHighPort); err == nil {
		t.Fatalf("expected invalid port error")
	}
}

func TestSelectPanelPortRejectsPrivilegedPorts(t *testing.T) {
	orig := unprivilegedPortStart
	unprivilegedPortStart = func() int { return DefaultUnprivilegedPortStart }
	t.Cleanup(func() { unprivilegedPortStart = orig })

	for _, port := range []int{80, 443, 1023} {
		_, _, err := SelectPanelPort(port, func() (int, error) { return 31874, nil })
		if err == nil || !strings.Contains(err.Error(), "privileged") {
			t.Fatalf("port %d: expected privileged-port error, got %v", port, err)
		}
	}
	port, random, err := SelectPanelPort(2096, func() (int, error) { return 31874, nil })
	if err != nil || port != 2096 || random {
		t.Fatalf("unprivileged port 2096 should be accepted, got port=%d random=%v err=%v", port, random, err)
	}
}

func TestSelectPanelPortHonorsLoweredUnprivilegedFloor(t *testing.T) {
	orig := unprivilegedPortStart
	unprivilegedPortStart = func() int { return 0 }
	t.Cleanup(func() { unprivilegedPortStart = orig })

	port, random, err := SelectPanelPort(80, func() (int, error) { return 31874, nil })
	if err != nil || port != 80 || random {
		t.Fatalf("port 80 should be accepted when unprivileged floor is 0, got port=%d random=%v err=%v", port, random, err)
	}
}

func TestSelectPanelPortReturnsErrorWhenRandomPortFails(t *testing.T) {
	sentinel := fmt.Errorf("random port failure")
	port, random, err := SelectPanelPort(0, func() (int, error) { return 0, sentinel })
	if err != sentinel {
		t.Fatalf("expected sentinel error %v, got err=%v port=%d random=%v", sentinel, err, port, random)
	}
}

func TestSelectPanelPortRejectsInvalidRandomPort(t *testing.T) {
	port, random, err := SelectPanelPort(0, func() (int, error) { return 0, nil })
	if err == nil {
		t.Fatalf("expected invalid random port error, got port=%d random=%v", port, random)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "invalid") {
		t.Fatalf("expected error to contain 'invalid', got: %v", err)
	}
}

func TestRandomHighPortIsInExpectedRange(t *testing.T) {
	port, err := RandomHighPort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port < 20000 || port > 50000 {
		t.Fatalf("expected port in 20000..50000, got %d", port)
	}
}
