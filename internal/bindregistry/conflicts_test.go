package bindregistry

import "testing"

func TestValidateNoConflicts(t *testing.T) {
	owners := map[BindKey]BindOwner{
		{Address: "0.0.0.0", Port: 443, Network: ListenTCP}:      {Kind: BindOwnerPanelCaddy},
		{Address: "192.168.1.10", Port: 443, Network: ListenTCP}: {Kind: BindOwnerNaive, InboundName: "naive-1"},
	}
	conflicts := ValidateNoConflicts(owners)
	// Exactly one conflict on the wildcard key, naming both owners — a
	// len>0 check greens a conflict reported on the wrong key or missing the
	// inbound identity (#901).
	if len(conflicts) != 1 {
		t.Fatalf("expected exactly one conflict for TCP 443 wildcard/specific overlap, got %+v", conflicts)
	}
	c := conflicts[0]
	wantKey := BindKey{Address: "0.0.0.0", Port: 443, Network: ListenTCP}
	if c.Key != wantKey {
		t.Fatalf("conflict key = %+v, want %+v", c.Key, wantKey)
	}
	if len(c.Owners) != 2 {
		t.Fatalf("conflict owners = %+v, want panel+naive", c.Owners)
	}
	if c.Owners[0].Kind != BindOwnerPanelCaddy {
		t.Error("first owner should be Panel Caddy")
	}
	if c.Owners[1].Kind != BindOwnerNaive || c.Owners[1].InboundName != "naive-1" {
		t.Errorf("second owner = %+v, want naive-1", c.Owners[1])
	}
	if c.Message != "tcp 0.0.0.0:443 is claimed by multiple owners" {
		t.Errorf("conflict message = %q", c.Message)
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
