package installer

import (
	"fmt"
	"testing"
)

func TestRandomHighPortPropagatesReaderError(t *testing.T) {
	orig := randomReader
	defer func() { randomReader = orig }()

	sentinel := fmt.Errorf("random reader failure")
	randomReader = func([]byte) (int, error) { return 0, sentinel }

	_, err := RandomHighPort()
	if err != sentinel {
		t.Fatalf("expected sentinel error %v, got %v", sentinel, err)
	}
}

func TestSelectPanelPortUsesDefaultRandomHighPortWhenNil(t *testing.T) {
	port, random, err := SelectPanelPort(0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !random {
		t.Fatalf("expected random port selection")
	}
	if port < RandomPortMin || port > RandomPortMax {
		t.Fatalf("port %d outside expected range [%d,%d]", port, RandomPortMin, RandomPortMax)
	}
}
