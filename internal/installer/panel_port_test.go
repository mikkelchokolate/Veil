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
