package installer

import "testing"

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
