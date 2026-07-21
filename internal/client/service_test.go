package client

import (
	"errors"
	"testing"
)

// TestValidateQuotaPrecisionCap pins the issue-3 contract: quotaBytes crosses
// the API as a JSON number, so values above Number.MAX_SAFE_INTEGER would
// silently lose precision in every JS consumer. The backend rejects them at
// the service-validation layer (in addition to the OpenAPI maximum and the
// SPA form schemas).
func TestValidateQuotaPrecisionCap(t *testing.T) {
	at := func(v int64) *int64 { return &v }
	cases := []struct {
		name    string
		quota   *int64
		wantErr bool
	}{
		{"nil (unlimited)", nil, false},
		{"zero", at(0), false},
		{"small", at(1 << 30), false},
		{"exactly MAX_SAFE_INTEGER", at(MaxQuotaBytes), false},
		{"MAX_SAFE_INTEGER + 1", at(MaxQuotaBytes + 1), true},
		{"far beyond", at(1 << 62), true},
		{"negative", at(-1), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validate(Client{Name: "x", QuotaBytes: tc.quota, QuotaResetPolicy: ResetNever})
			if tc.wantErr && !errors.Is(err, ErrValidation) {
				t.Fatalf("quota %v: expected ErrValidation, got %v", tc.quota, err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("quota %v: unexpected error: %v", tc.quota, err)
			}
		})
	}
	if MaxQuotaBytes != 1<<53-1 {
		t.Fatalf("MaxQuotaBytes = %d, want 2^53-1 (Number.MAX_SAFE_INTEGER)", MaxQuotaBytes)
	}
}
