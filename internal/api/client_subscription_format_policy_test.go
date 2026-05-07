package api

import "testing"

func TestClientSubscriptionFormatPolicyNormalizesDefaultAndAllowedFormats(t *testing.T) {
	policy := NewClientSubscriptionFormatPolicy()
	for _, tc := range []struct{ in, want string }{{"", "base64"}, {"base64", "base64"}, {"raw", "raw"}} {
		got, err := policy.Normalize(tc.in)
		if err != nil {
			t.Fatalf("Normalize(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("Normalize(%q) = %q", tc.in, got)
		}
	}
}

func TestClientSubscriptionFormatPolicyRejectsUnsupportedFormat(t *testing.T) {
	_, err := NewClientSubscriptionFormatPolicy().Normalize("json")
	if err == nil || err.Error() != "format must be base64 or raw" {
		t.Fatalf("err = %v", err)
	}
}
