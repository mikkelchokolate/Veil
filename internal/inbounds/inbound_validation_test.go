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

func TestInboundValidationUpdateDoesNotRequireName(t *testing.T) {
	validator := NewInboundValidation()
	if err := validator.ValidateUpdate(Inbound{Protocol: "naiveproxy", Transport: "tcp", Port: 443}); err != nil {
		t.Fatalf("ValidateUpdate: %v", err)
	}
}
