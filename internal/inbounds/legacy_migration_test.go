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

func TestDetectLegacyInboundsIgnoresNonNaiveAndMigrated(t *testing.T) {
	inbounds := []model.Inbound{
		{Name: "old", Protocol: "naiveproxy", Port: 443, ProtocolFields: map[string]any{}},
		{Name: "migrated", Protocol: "naiveproxy", Port: 443, ProtocolFields: map[string]any{"domain": "vpn.example.com"}},
		{Name: "hy2", Protocol: "hysteria2", Port: 443, ProtocolFields: map[string]any{}},
	}
	legacy := DetectLegacyInbounds(inbounds)
	if len(legacy) != 1 || legacy[0].Name != "old" {
		t.Fatalf("expected only 'old' legacy inbound, got %+v", legacy)
	}
	if CanCreateManagedNaive(inbounds) {
		t.Error("managed naive creation must be blocked while any legacy inbound remains")
	}
}

func TestCanCreateManagedNaiveWhenNoLegacy(t *testing.T) {
	inbounds := []model.Inbound{
		{Name: "migrated", Protocol: "naiveproxy", Port: 443, ProtocolFields: map[string]any{"domain": "vpn.example.com"}},
		{Name: "hy2", Protocol: "hysteria2", Port: 443, ProtocolFields: map[string]any{}},
	}
	if !CanCreateManagedNaive(inbounds) {
		t.Error("managed naive creation should be allowed when no legacy inbounds exist")
	}
}
