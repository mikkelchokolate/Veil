package panelmaterial

import (
	"strings"
	"testing"
)

// The controlled-CA settings must persist into veil.env so the running panel
// (EnvironmentFile) keeps rendering Caddy issuers against the same ACME
// directory after the install-time environment is gone (audit #304).
func TestEnvContentPersistsControlledCA(t *testing.T) {
	t.Parallel()

	m := NewManagedMaterial(Input{
		Paths:          Paths{EtcDir: t.TempDir()},
		PanelAuthToken: "tok",
		ACMECAURL:      "https://127.0.0.1:14000/dir",
		ACMECARoot:     "/etc/veil/acme-root.pem",
	})
	env, err := m.EnvContent()
	if err != nil {
		t.Fatalf("EnvContent: %v", err)
	}
	for _, want := range []string{
		"VEIL_ACME_CA_URL=https://127.0.0.1:14000/dir\n",
		"VEIL_ACME_CA_ROOT=/etc/veil/acme-root.pem\n",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("EnvContent missing %q:\n%s", want, env)
		}
	}
}

func TestEnvContentOmitsCAWhenUnset(t *testing.T) {
	t.Parallel()

	m := NewManagedMaterial(Input{
		Paths:          Paths{EtcDir: t.TempDir()},
		PanelAuthToken: "tok",
	})
	env, err := m.EnvContent()
	if err != nil {
		t.Fatalf("EnvContent: %v", err)
	}
	if strings.Contains(env, "VEIL_ACME_CA_URL") || strings.Contains(env, "VEIL_ACME_CA_ROOT") {
		t.Fatalf("EnvContent must not emit CA settings when unset:\n%s", env)
	}
}
