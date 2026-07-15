package bindregistry

import (
	"strings"
	"testing"
)

func TestValidateNoConflictsReportsWildcardCollision(t *testing.T) {
	t.Parallel()

	owners := map[BindKey]BindOwner{
		{Address: "0.0.0.0", Port: 443, Network: ListenTCP}: {
			Kind:        BindOwnerPanelCaddy,
			ServiceName: "veil-caddy.service",
		},
		{Address: "192.0.2.10", Port: 443, Network: ListenTCP}: {
			Kind:        BindOwnerNaive,
			InboundName: "naive-main",
		},
	}

	conflicts := ValidateNoConflicts(owners)
	if len(conflicts) != 1 {
		t.Fatalf("expected one conflict, got %#v", conflicts)
	}
	if len(conflicts[0].Owners) != 2 {
		t.Fatalf("expected two owners, got %#v", conflicts[0].Owners)
	}
	for _, fragment := range []string{"tcp", "443", "panel_caddy", "naive-main"} {
		if !strings.Contains(conflicts[0].Message, fragment) {
			t.Errorf("conflict message %q does not contain %q", conflicts[0].Message, fragment)
		}
	}
}

func TestValidateNoConflictsAllowsTCPAndUDPReuse(t *testing.T) {
	t.Parallel()

	owners := map[BindKey]BindOwner{
		{Address: "0.0.0.0", Port: 443, Network: ListenTCP}: {Kind: BindOwnerPanelCaddy},
		{Address: "0.0.0.0", Port: 443, Network: ListenUDP}: {
			Kind:        BindOwnerHysteria2,
			InboundName: "hy-main",
		},
	}
	if conflicts := ValidateNoConflicts(owners); len(conflicts) != 0 {
		t.Fatalf("expected no TCP/UDP conflict, got %#v", conflicts)
	}
}

func TestValidateNoConflictsCanonicalizesEquivalentKeys(t *testing.T) {
	t.Parallel()

	owners := map[BindKey]BindOwner{
		{Address: "", Port: 8443, Network: "TCP"}: {
			Kind:        BindOwnerNaive,
			InboundName: "first",
		},
		{Address: "0.0.0.0", Port: 8443, Network: ListenTCP}: {
			Kind:        BindOwnerNaive,
			InboundName: "second",
		},
	}
	if conflicts := ValidateNoConflicts(owners); len(conflicts) != 1 {
		t.Fatalf("expected equivalent keys to conflict, got %#v", conflicts)
	}
}

func TestValidateNoConflictsStableOrder(t *testing.T) {
	t.Parallel()

	owners := map[BindKey]BindOwner{
		{Address: "0.0.0.0", Port: 443, Network: ListenTCP}: {Kind: BindOwnerPanelCaddy},
		{Address: "192.0.2.10", Port: 443, Network: ListenTCP}: {Kind: BindOwnerNaive, InboundName: "a"},
		{Address: "192.0.2.11", Port: 443, Network: ListenTCP}: {Kind: BindOwnerNaive, InboundName: "b"},
	}

	first := ValidateNoConflicts(owners)
	second := ValidateNoConflicts(owners)
	if len(first) != len(second) {
		t.Fatalf("unstable result length: %d != %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Message != second[i].Message {
			t.Fatalf("unstable result at %d: %q != %q", i, first[i].Message, second[i].Message)
		}
	}
}
