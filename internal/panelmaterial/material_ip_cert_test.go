package panelmaterial

import (
	"strings"
	"testing"
)

// Issues #1186/#1187/#1189: the install-time IP-certificate lifecycle
// choices must land in veil.env so the daemon renewal worker, the apply
// post-hook and `veil cert renew` all reuse them once the install shell is
// gone.
func TestEnvContentPersistsIPCertLifecycle(t *testing.T) {
	t.Parallel()

	m := NewManagedMaterial(Input{
		Paths:           Paths{EtcDir: t.TempDir()},
		PanelAuthToken:  "tok",
		PanelAccess:     "direct",
		PanelPublicIP:   "203.0.113.9,2001:db8::7",
		PanelLEIPCert:   "0",
		PanelHTTP01Port: "8080",
		ACMEInsecure:    true,
	})
	env, err := m.EnvContent()
	if err != nil {
		t.Fatalf("EnvContent: %v", err)
	}
	for _, want := range []string{
		"VEIL_PANEL_PUBLIC_IP=203.0.113.9,2001:db8::7\n",
		"VEIL_PANEL_LE_IP_CERT=0\n",
		"VEIL_PANEL_HTTP01_PORT=8080\n",
		"VEIL_ACME_INSECURE=1\n",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("EnvContent missing %q:\n%s", want, env)
		}
	}
}

// Issue #1208: the recorded IP-certificate lifecycle knobs must survive an
// access-mode change — a direct→caddy→direct round trip dropping
// VEIL_PANEL_PUBLIC_IP silently disables renewal for a cert the install
// already issued. Emission keys on the values being present, not on the
// current mode.
func TestEnvContentKeepsIPCertKnobsAcrossAccessModes(t *testing.T) {
	t.Parallel()

	m := NewManagedMaterial(Input{
		Paths:          Paths{EtcDir: t.TempDir()},
		PanelAuthToken: "tok",
		PanelAccess:    "caddy",
		PanelPublicIP:  "203.0.113.9",
		PanelLEIPCert:  "0",
	})
	env, err := m.EnvContent()
	if err != nil {
		t.Fatalf("EnvContent: %v", err)
	}
	for _, want := range []string{"VEIL_PANEL_PUBLIC_IP=203.0.113.9\n", "VEIL_PANEL_LE_IP_CERT=0\n"} {
		if !strings.Contains(env, want) {
			t.Fatalf("access-mode change dropped %q:\n%s", want, env)
		}
	}
}

// Unset lifecycle knobs render no lines: an absent --le-ip-cert keeps the
// enabled-by-default contract and port 80 is the implicit default.
func TestEnvContentOmitsUnsetIPCertKnobs(t *testing.T) {
	t.Parallel()

	m := NewManagedMaterial(Input{
		Paths:           Paths{EtcDir: t.TempDir()},
		PanelAuthToken:  "tok",
		PanelAccess:     "direct",
		PanelHTTP01Port: "80",
	})
	env, err := m.EnvContent()
	if err != nil {
		t.Fatalf("EnvContent: %v", err)
	}
	for _, key := range []string{"VEIL_PANEL_PUBLIC_IP", "VEIL_PANEL_LE_IP_CERT", "VEIL_PANEL_HTTP01_PORT", "VEIL_ACME_INSECURE"} {
		if strings.Contains(env, key) {
			t.Fatalf("EnvContent emitted %s for an unset/default knob:\n%s", key, env)
		}
	}
}
