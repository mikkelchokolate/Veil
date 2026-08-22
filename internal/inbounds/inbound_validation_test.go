package inbounds

import "testing"

func TestInboundValidationCreateRequiresNameProtocolTransportAndPort(t *testing.T) {
	validator := NewInboundValidation()
	valid := Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443}
	if err := validator.ValidateCreate(valid); err != nil {
		t.Fatalf("ValidateCreate valid: %v", err)
	}
	for _, inbound := range []Inbound{
		{Protocol: "naiveproxy", Transport: "tcp", Port: 443},
		{Name: "naive", Transport: "tcp", Port: 443},
		{Name: "naive", Protocol: "naiveproxy", Port: 443},
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp"},
	} {
		if err := validator.ValidateCreate(inbound); err != ErrInboundInvalid {
			t.Fatalf("ValidateCreate(%+v) = %v", inbound, err)
		}
	}
}

func TestInboundValidationCreateRequiresSafeName(t *testing.T) {
	validator := NewInboundValidation()
	for _, name := range []string{"edge.v1", "edge v1", "edge/v1", "edge@v1"} {
		inbound := Inbound{Name: name, Protocol: "naiveproxy", Transport: "tcp", Port: 443}
		if err := validator.ValidateCreate(inbound); err != ErrInboundInvalid {
			t.Fatalf("ValidateCreate(%q) = %v, want ErrInboundInvalid", name, err)
		}
	}
	for _, name := range []string{"edge", "edge-v1", "edge_v1", "Edge01"} {
		inbound := Inbound{Name: name, Protocol: "naiveproxy", Transport: "tcp", Port: 443}
		if err := validator.ValidateCreate(inbound); err != nil {
			t.Fatalf("ValidateCreate(%q) = %v", name, err)
		}
	}
}

func TestInboundValidationUpdateRequiresSafeName(t *testing.T) {
	validator := NewInboundValidation()
	if err := validator.ValidateUpdate(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 443}); err != nil {
		t.Fatalf("ValidateUpdate valid: %v", err)
	}
	for _, name := range []string{"", "../naive", "naive/service", "naive%25"} {
		inbound := Inbound{Name: name, Protocol: "naiveproxy", Transport: "tcp", Port: 443}
		if err := validator.ValidateUpdate(inbound); err != ErrInboundInvalid {
			t.Fatalf("ValidateUpdate(%q) = %v, want ErrInboundInvalid", name, err)
		}
	}
}

// TestInboundValidationRejectsPortsAbove65535 locks in audit #21/#98: only
// ports in [1, 65535] are valid; larger values used to sail into the renderer
// as "listen: :70000".
func TestInboundValidationRejectsPortsAbove65535(t *testing.T) {
	validator := NewInboundValidation()
	for _, inbound := range []Inbound{
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 65536},
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 70000},
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: -1},
		{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 0},
	} {
		if err := validator.ValidateCreate(inbound); err != ErrInboundInvalid {
			t.Fatalf("ValidateCreate(Port=%d) = %v, want ErrInboundInvalid", inbound.Port, err)
		}
		if err := validator.ValidateUpdate(inbound); err != ErrInboundInvalid {
			t.Fatalf("ValidateUpdate(Port=%d) = %v, want ErrInboundInvalid", inbound.Port, err)
		}
	}
	if err := validator.ValidateCreate(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 65535}); err != nil {
		t.Fatalf("ValidateCreate(Port=65535) = %v, want valid", err)
	}
	if err := validator.ValidateCreate(Inbound{Name: "naive", Protocol: "naiveproxy", Transport: "tcp", Port: 1}); err != nil {
		t.Fatalf("ValidateCreate(Port=1) = %v, want valid", err)
	}
}
