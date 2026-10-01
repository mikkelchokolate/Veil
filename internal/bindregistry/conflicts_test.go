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

// #1225: distinct raw keys that canonicalize to the same bind ("" and
// "0.0.0.0" both mean the IPv4 wildcard) must produce a conflict when
// claimed by different owners — a last-writer-wins collapse would silently
// lose one owner and let a real double-bind pass validation.
func TestValidateNoConflictsCanonicalCollapse(t *testing.T) {
	owners := map[BindKey]BindOwner{
		{Address: "", Port: 443, Network: ListenTCP}:        {Kind: BindOwnerPanelCaddy},
		{Address: "0.0.0.0", Port: 443, Network: ListenTCP}: {Kind: BindOwnerNaive, InboundName: "naive-1"},
	}
	conflicts := ValidateNoConflicts(owners)
	if len(conflicts) != 1 {
		t.Fatalf("expected exactly one conflict for the canonical wildcard collapse, got %+v", conflicts)
	}
	c := conflicts[0]
	wantKey := BindKey{Address: "0.0.0.0", Port: 443, Network: ListenTCP}
	if c.Key != wantKey {
		t.Fatalf("conflict key = %+v, want %+v", c.Key, wantKey)
	}
	if len(c.Owners) != 2 {
		t.Fatalf("conflict owners = %+v, want panel+naive", c.Owners)
	}
	kinds := map[BindOwnerKind]bool{}
	for _, o := range c.Owners {
		kinds[o.Kind] = true
	}
	if !kinds[BindOwnerPanelCaddy] || !kinds[BindOwnerNaive] {
		t.Fatalf("conflict must name both owners: %+v", c.Owners)
	}

	// The same owner claiming equivalent raw keys is NOT a conflict — the
	// collapse only reports distinct owners.
	sameOwner := map[BindKey]BindOwner{
		{Address: "", Port: 8443, Network: ListenUDP}:        {Kind: BindOwnerHysteria2, InboundName: "hy"},
		{Address: "0.0.0.0", Port: 8443, Network: ListenUDP}: {Kind: BindOwnerHysteria2, InboundName: "hy"},
	}
	if conflicts := ValidateNoConflicts(sameOwner); len(conflicts) != 0 {
		t.Fatalf("same owner on equivalent raw keys must not conflict, got %+v", conflicts)
	}
}
