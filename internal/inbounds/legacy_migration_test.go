package inbounds

import (
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestDetectLegacyInbound(t *testing.T) {
	inbounds := []model.Inbound{
		{Name: "old", Protocol: "naiveproxy", Port: 443, ProtocolFields: map[string]any{}},
	}
	legacy := DetectLegacyInbounds(inbounds)
	if len(legacy) != 1 {
		t.Fatalf("expected 1 legacy inbound, got %d", len(legacy))
	}
}

func TestBlockManagedUntilLegacyResolved(t *testing.T) {
	inbounds := []model.Inbound{
		{Name: "old", Protocol: "naiveproxy", Port: 443, ProtocolFields: map[string]any{}},
	}
	if CanCreateManagedNaive(inbounds) {
		t.Error("creating managed naive inbounds must be blocked while legacy exists")
	}
}
