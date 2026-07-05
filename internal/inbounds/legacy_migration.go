package inbounds

import "github.com/mikkelchokolate/Veil/internal/model"

// LegacyState describes the migration status of an inbound.
type LegacyState string

// DetectLegacyInbounds returns naiveproxy inbounds that do not have per-inbound
// domain configuration in ProtocolFields.
func DetectLegacyInbounds(inbounds []model.Inbound) []model.Inbound {
	var out []model.Inbound
	for _, inb := range inbounds {
		if inb.Protocol != "naiveproxy" {
			continue
		}
		if inb.ProtocolFields == nil || inb.ProtocolFields["domain"] == nil || inb.ProtocolFields["domain"] == "" {
			out = append(out, inb)
		}
	}
	return out
}

// CanCreateManagedNaive reports whether new managed naive inbounds may be
// created. Creation is blocked while any legacy naiveproxy inbounds exist.
func CanCreateManagedNaive(inbounds []model.Inbound) bool {
	return len(DetectLegacyInbounds(inbounds)) == 0
}

// SuggestMigration fills in domain/publicPort/transport/email for legacy
// naiveproxy inbounds using the provided settings. The returned inbounds still
// require admin review before applying.
//
// Implementation is tracked in Task 19; this function currently returns the
// input unchanged.
func SuggestMigration(settings model.Settings, inbounds []model.Inbound) ([]model.Inbound, error) {
	// Populate domain/publicPort/transport/email; require admin review before applying.
	// Implementation in Task 19 UI/API task.
	_ = settings
	return inbounds, nil
}
