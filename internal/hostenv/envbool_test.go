package hostenv

import "testing"

// TestEnvBoolFailsClosed is the #1263 regression: security booleans must
// follow strconv.ParseBool, so "0"/"false"/typos never enable them.
// Malformed input fails closed. "no"/"off" are false only because
// strconv.ParseBool rejects them as malformed — they are not accepted
// spellings, and must not be "fixed" into ones that could enable a knob.
func TestEnvBoolMalformedInputFailsClosed(t *testing.T) {
	for value, want := range map[string]bool{
		"":        false,
		"0":       false,
		"false":   false,
		"FALSE":   false,
		"no":      false,
		"off":     false,
		"garbage": false,
		"1":       true,
		" 1 ":     true,
		"true":    true,
		"TRUE":    true,
		"t":       true,
	} {
		if got := EnvBool(value); got != want {
			t.Errorf("EnvBool(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestEnvBoolOrFileProcessEnvWins(t *testing.T) {
	persisted := map[string]string{"VEIL_ACME_INSECURE": "1"}
	t.Setenv("VEIL_ACME_INSECURE", "0")
	if EnvBoolOrFile(persisted, "VEIL_ACME_INSECURE") {
		t.Fatal("explicit VEIL_ACME_INSECURE=0 must override a persisted =1")
	}
	t.Setenv("VEIL_ACME_INSECURE", "")
	if !EnvBoolOrFile(persisted, "VEIL_ACME_INSECURE") {
		t.Fatal("persisted VEIL_ACME_INSECURE=1 must apply when the env is unset")
	}
	if EnvBoolOrFile(map[string]string{"VEIL_ACME_INSECURE": "0"}, "VEIL_ACME_INSECURE") {
		t.Fatal("persisted VEIL_ACME_INSECURE=0 must not enable insecure ACME")
	}
}
