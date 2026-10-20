package installer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/runtimeports"
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

// #1309: an explicit --panel-port (or interactive custom port) on one of
// Veil's own loopback service ports must be refused at install time, exactly
// like management-state validation refuses it as settings.panelListen. The
// literal list keeps the test from passing vacuously if the runtimeports
// table ever shrinks.
func TestSelectPanelPortRejectsReservedInternalPorts(t *testing.T) {
	orig := unprivilegedPortStart
	unprivilegedPortStart = func() int { return DefaultUnprivilegedPortStart }
	t.Cleanup(func() { unprivilegedPortStart = orig })

	wantOwner := map[int]string{
		2019:  "Caddy admin",
		44080: "HTTP-01",
		61000: "traffic statistics",
		61001: "auth callback",
	}
	for _, port := range runtimeports.ReservedInternalTCPPorts() {
		if _, ok := wantOwner[port]; !ok {
			t.Fatalf("reserved port %d has no expectation in this test; extend wantOwner", port)
		}
	}
	for port, owner := range wantOwner {
		got, random, err := SelectPanelPort(port, func() (int, error) { return 31874, nil })
		if err == nil {
			t.Fatalf("requested port %d: expected reserved-port error, got port=%d random=%v", port, got, random)
		}
		if !strings.Contains(err.Error(), "reserved") || !strings.Contains(err.Error(), owner) {
			t.Fatalf("requested port %d: error must name the owning service %q, got %v", port, owner, err)
		}
		if got != 0 || random {
			t.Fatalf("requested port %d: rejected selection must return zero values, got port=%d random=%v", port, got, random)
		}

		// A random-port source that ever yields a reserved port must not
		// slip it through either.
		reserved := port
		got, random, err = SelectPanelPort(0, func() (int, error) { return reserved, nil })
		if err == nil || !strings.Contains(err.Error(), owner) {
			t.Fatalf("random port %d: expected reserved-port error naming %q, got port=%d random=%v err=%v", port, owner, got, random, err)
		}
	}

	// Neighbours of reserved ports stay selectable.
	for _, port := range []int{2018, 2020, 44079, 44081, 60999, 61002} {
		got, random, err := SelectPanelPort(port, func() (int, error) { return 31874, nil })
		if err != nil || got != port || random {
			t.Fatalf("port %d should be accepted, got port=%d random=%v err=%v", port, got, random, err)
		}
	}
}

func TestBuildRURecommendedInstallRejectsReservedPanelPort(t *testing.T) {
	for _, port := range runtimeports.ReservedInternalTCPPorts() {
		_, err := BuildRURecommendedInstall(RURecommendedInstallInput{
			Domain:      "example.com",
			Email:       "admin@example.com",
			PanelAccess: "direct",
			PanelPort:   port,
		})
		if err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Fatalf("panel port %d: expected reserved-port error, got %v", port, err)
		}
	}
}
