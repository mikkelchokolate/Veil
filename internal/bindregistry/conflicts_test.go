package bindregistry

import "testing"

func TestValidateNoConflicts(t *testing.T) {
	owners := map[BindKey]BindOwner{
		{Address: "0.0.0.0", Port: 443, Network: ListenTCP}:      {Kind: BindOwnerPanelCaddy},
		{Address: "192.168.1.10", Port: 443, Network: ListenTCP}: {Kind: BindOwnerNaive, InboundName: "naive-1"},
	}
	conflicts := ValidateNoConflicts(owners)
	if len(conflicts) == 0 {
		t.Fatal("expected conflict between wildcard Panel and specific naive on TCP 443")
	}
	if conflicts[0].Owners[0].Kind != BindOwnerPanelCaddy {
		t.Error("first owner should be Panel Caddy")
	}
}

func TestValidateNoConflictsNoCollision(t *testing.T) {
	owners := map[BindKey]BindOwner{
		{Address: "0.0.0.0", Port: 443, Network: ListenTCP}: {Kind: BindOwnerPanelCaddy},
		{Address: "0.0.0.0", Port: 443, Network: ListenUDP}: {Kind: BindOwnerHysteria2},
	}
	if conflicts := ValidateNoConflicts(owners); len(conflicts) != 0 {
		t.Fatalf("expected no TCP/UDP conflict, got %v", conflicts)
	}
}
