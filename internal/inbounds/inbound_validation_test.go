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
