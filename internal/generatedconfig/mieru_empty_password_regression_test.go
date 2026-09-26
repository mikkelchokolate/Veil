package generatedconfig

import "testing"

// TestMieruGeneratedConfigModelRejectsEmptyPasswordFallback covers #1062:
// an enabled mieru inbound with no credentials (e.g. restored from an old
// backup that predates credential_required live validation) used to mint a
// fallback user carrying the inbound name and an EMPTY password — anyone who
// can guess the inbound name could authenticate. The client-access
// aggregator already skips the same fallback when the password resolves
// empty; the server model must fail the build honestly instead, matching
// RenderMieru's user check ("mieru user name and password are required").
func TestMieruGeneratedConfigModelRejectsEmptyPasswordFallback(t *testing.T) {
	_, _, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{
		{Name: "mieru-tcp", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true},
	})
	if err == nil || err.Error() != "mieru user name and password are required" {
		t.Fatalf("Build err = %v, want mieru credential error", err)
	}
}

// TestMieruGeneratedConfigModelRejectsEmptyPasswordFallbackAmongSiblings
// pins the aggregated case: the poisoned fallback must fail the whole render
// rather than ride along inside a config whose other inbounds are healthy —
// RenderMieru's per-user check would otherwise be the only (later) guard.
func TestMieruGeneratedConfigModelRejectsEmptyPasswordFallbackAmongSiblings(t *testing.T) {
	_, _, err := NewMieruGeneratedConfigModel(Settings{}).Build([]Inbound{
		{Name: "healthy", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true, Password: "real-pass"},
		{Name: "broken", Protocol: "mieru", Transport: "udp", Port: 444, Enabled: true},
	})
	if err == nil || err.Error() != "mieru user name and password are required" {
		t.Fatalf("Build err = %v, want mieru credential error", err)
	}
}
