package client

import "testing"

// TestRuntimeEligibleMatchesRenderAdmission (#1177): the shared gate must
// mirror the render admission filter exactly — enabled, not depleted, and
// expires_at strictly after now (an expiry AT now is already expired, as
// the render filter, ComputeStatus, and ListRuntimeBindingsForInbound all
// agree).
func TestRuntimeEligibleMatchesRenderAdmission(t *testing.T) {
	const now int64 = 1_000_000
	past := now - 1
	at := now
	future := now + 1
	cases := []struct {
		name      string
		enabled   bool
		depleted  bool
		expiresAt *int64
		want      bool
	}{
		{"enabled no expiry", true, false, nil, true},
		{"enabled future expiry", true, false, &future, true},
		{"disabled", false, false, nil, false},
		{"depleted", true, true, nil, false},
		{"expired", true, false, &past, false},
		{"expiry boundary equals now", true, false, &at, false},
		{"disabled depleted expired", false, true, &past, false},
	}
	for _, tc := range cases {
		if got := RuntimeEligible(tc.enabled, tc.depleted, tc.expiresAt, now); got != tc.want {
			t.Fatalf("RuntimeEligible(%s)=%v, want %v", tc.name, got, tc.want)
		}
		c := Client{Enabled: tc.enabled, Depleted: tc.depleted, ExpiresAt: tc.expiresAt}
		if got := c.RuntimeEligible(now); got != tc.want {
			t.Fatalf("Client.RuntimeEligible(%s)=%v, want %v", tc.name, got, tc.want)
		}
	}
}
