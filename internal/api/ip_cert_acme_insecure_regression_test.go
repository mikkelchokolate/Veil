package api

import "testing"

// #1334 review: the panel-side IP-cert config parses VEIL_ACME_INSECURE as a
// bool; "0" or a malformed value must not enable insecure ACME.
func TestIPCertACMEConfigParsesInsecureAsBool(t *testing.T) {
	for value, want := range map[string]bool{"": false, "0": false, "false": false, "bogus": false, "1": true, "true": true, " 1 ": true} {
		stubIPCertEnv(t, map[string]string{"VEIL_ACME_INSECURE": value})
		if _, got, _ := ipCertACMEConfig(); got != want {
			t.Errorf("VEIL_ACME_INSECURE=%q: insecure = %v, want %v", value, got, want)
		}
	}
}
