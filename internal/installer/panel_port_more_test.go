package installer

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/runtimeports"
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

// fixedRandomReader returns a reader that encodes n big-endian, so
// RandomHighPort's draw index is exactly n modulo the span.
func fixedRandomReader(n uint64) func([]byte) (int, error) {
	return func(b []byte) (int, error) {
		for i := range b {
			b[i] = 0
		}
		binary.BigEndian.PutUint64(b[len(b)-8:], n)
		return len(b), nil
	}
}

// TestRandomHighPortSkipsReservedInternalPorts is the #1285 regression: the
// random panel port range contains the parked acme.sh HTTP-01 port 44080; a
// panel landing there would make every Caddy-fronted IP-cert renewal fail.
// The index that used to map to 44080 must now map past it, and the whole
// range must stay reachable without ever yielding a reserved port.
func TestRandomHighPortSkipsReservedInternalPorts(t *testing.T) {
	orig := randomReader
	t.Cleanup(func() { randomReader = orig })

	reserved := runtimeports.ACMEHTTP01ParkedPort
	if reserved < RandomPortMin || reserved > RandomPortMax {
		t.Fatalf("test premise: %d must lie inside the random range [%d,%d]", reserved, RandomPortMin, RandomPortMax)
	}

	randomReader = fixedRandomReader(uint64(reserved - RandomPortMin))
	port, err := RandomHighPort()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != reserved+1 {
		t.Fatalf("index of reserved port %d must shift to %d, got %d", reserved, reserved+1, port)
	}

	randomReader = fixedRandomReader(uint64(reserved - RandomPortMin - 1))
	if port, err := RandomHighPort(); err != nil || port != reserved-1 {
		t.Fatalf("index below the reserved port must be unchanged: got %d, %v", port, err)
	}

	randomReader = fixedRandomReader(0)
	if port, err := RandomHighPort(); err != nil || port != RandomPortMin {
		t.Fatalf("index 0 must map to RandomPortMin %d, got %d, %v", RandomPortMin, port, err)
	}

	// The last index of the shrunken span reaches RandomPortMax; one more
	// wraps back to RandomPortMin.
	span := uint64(RandomPortMax - RandomPortMin + 1 - len(reservedPortsInRandomRange()))
	randomReader = fixedRandomReader(span - 1)
	if port, err := RandomHighPort(); err != nil || port != RandomPortMax {
		t.Fatalf("last index must map to RandomPortMax %d, got %d, %v", RandomPortMax, port, err)
	}
	randomReader = fixedRandomReader(span)
	if port, err := RandomHighPort(); err != nil || port != RandomPortMin {
		t.Fatalf("index == span must wrap to RandomPortMin %d, got %d, %v", RandomPortMin, port, err)
	}
}

// TestRandomHighPortNeverYieldsReservedPort sweeps a window of indices around
// every reserved port inside the range and checks the result is in range,
// never reserved, and strictly increasing (i.e. the mapping is injective).
func TestRandomHighPortNeverYieldsReservedPort(t *testing.T) {
	orig := randomReader
	t.Cleanup(func() { randomReader = orig })

	for _, r := range reservedPortsInRandomRange() {
		prev := 0
		for idx := r - RandomPortMin - 3; idx <= r-RandomPortMin+3; idx++ {
			if idx < 0 {
				continue
			}
			randomReader = fixedRandomReader(uint64(idx))
			port, err := RandomHighPort()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if port < RandomPortMin || port > RandomPortMax {
				t.Fatalf("index %d: port %d outside [%d,%d]", idx, port, RandomPortMin, RandomPortMax)
			}
			if runtimeports.IsReservedInternalTCPPort(port) {
				t.Fatalf("index %d yielded reserved internal port %d", idx, port)
			}
			if port <= prev {
				t.Fatalf("index %d: mapping not strictly increasing (%d after %d)", idx, port, prev)
			}
			prev = port
		}
	}
}
