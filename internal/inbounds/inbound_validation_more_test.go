package inbounds

import "testing"

func TestInboundValidationCreateRejectsUnsupportedCombinations(t *testing.T) {
	validator := NewInboundValidation()
	cases := []struct {
		name      string
		inbound   Inbound
		wantError error
	}{
		{
			name:      "unsupported transport for naiveproxy",
			inbound:   Inbound{Name: "n", Protocol: "naiveproxy", Transport: "udp", Port: 443},
			wantError: ErrInboundUnsupportedProtocolTransport,
		},
		{
			name:      "unsupported transport for hysteria2",
			inbound:   Inbound{Name: "n", Protocol: "hysteria2", Transport: "tcp", Port: 443},
			wantError: ErrInboundUnsupportedProtocolTransport,
		},
		{
			name:      "unknown protocol",
			inbound:   Inbound{Name: "n", Protocol: "unknown", Transport: "tcp", Port: 443},
			wantError: ErrInboundUnsupportedProtocolTransport,
		},
		{
			name:      "mieru tcp is supported",
			inbound:   Inbound{Name: "n", Protocol: "mieru", Transport: "tcp", Port: 443},
			wantError: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validator.ValidateCreate(tc.inbound)
			if err != tc.wantError {
				t.Fatalf("ValidateCreate error = %v, want %v", err, tc.wantError)
			}
		})
	}
}

func TestInboundValidationUpdateRejectsInvalidAndUnsupported(t *testing.T) {
	validator := NewInboundValidation()
	cases := []struct {
		name      string
		inbound   Inbound
		wantError error
	}{
		{
			name:      "missing name",
			inbound:   Inbound{Protocol: "naiveproxy", Transport: "tcp", Port: 443},
			wantError: ErrInboundInvalid,
		},
		{
			name:      "unsafe name",
			inbound:   Inbound{Name: "../escape", Protocol: "naiveproxy", Transport: "tcp", Port: 443},
			wantError: ErrInboundInvalid,
		},
		{
			name:      "missing protocol",
			inbound:   Inbound{Name: "n", Transport: "tcp", Port: 443},
			wantError: ErrInboundInvalid,
		},
		{
			name:      "missing transport",
			inbound:   Inbound{Name: "n", Protocol: "naiveproxy", Port: 443},
			wantError: ErrInboundInvalid,
		},
		{
			name:      "invalid port",
			inbound:   Inbound{Name: "n", Protocol: "naiveproxy", Transport: "tcp", Port: 0},
			wantError: ErrInboundInvalid,
		},
		{
			name:      "unsupported transport",
			inbound:   Inbound{Name: "n", Protocol: "naiveproxy", Transport: "udp", Port: 443},
			wantError: ErrInboundUnsupportedProtocolTransport,
		},
		{
			name:      "unknown protocol",
			inbound:   Inbound{Name: "n", Protocol: "unknown", Transport: "tcp", Port: 443},
			wantError: ErrInboundUnsupportedProtocolTransport,
		},
		{
			name:      "valid update",
			inbound:   Inbound{Name: "safe_name-1", Protocol: "hysteria2", Transport: "udp", Port: 8443},
			wantError: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validator.ValidateUpdate(tc.inbound)
			if err != tc.wantError {
				t.Fatalf("ValidateUpdate error = %v, want %v", err, tc.wantError)
			}
		})
	}
}
