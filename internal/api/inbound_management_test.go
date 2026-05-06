package api

import "testing"

func TestInboundManagementCreateMutatesAndSaves(t *testing.T) {
	inbounds := []Inbound{}
	saves := 0
	management := NewInboundManagement(&inbounds, func() error {
		saves++
		return nil
	})

	created, err := management.Create(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443, Enabled: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Password == "" {
		t.Fatalf("expected generated password")
	}
	if len(inbounds) != 1 || inbounds[0].Name != "naive" {
		t.Fatalf("inbounds not updated: %+v", inbounds)
	}
	if saves != 1 {
		t.Fatalf("saves = %d, want 1", saves)
	}
}

func TestInboundManagementDoesNotSaveOnMutationError(t *testing.T) {
	inbounds := []Inbound{{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443}}
	saves := 0
	management := NewInboundManagement(&inbounds, func() error {
		saves++
		return nil
	})

	_, err := management.Create(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 9443})
	if err == nil {
		t.Fatalf("expected duplicate name error")
	}
	if saves != 0 {
		t.Fatalf("saves = %d, want 0", saves)
	}
}
