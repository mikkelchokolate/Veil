package panelmaterial

import (
	"strings"
	"testing"
)

// TestEnvContentRejectsControlCharacters is the #1023 regression: veil.env is
// a raw KEY=value file sourced by the root-run veil-backup.service
// (EnvironmentFile), so a newline inside any value (e.g. a PanelListen read
// back from state) would terminate the assignment and inject arbitrary env
// lines. There is no escape — the render must fail closed.
func TestEnvContentRejectsControlCharacters(t *testing.T) {
	for _, input := range []Input{
		{PanelAuthToken: "tok", PanelListen: "127.0.0.1:2096\nVEIL_EVIL=1"},
		{PanelAuthToken: "tok", Domain: "vpn.example.com\rVEIL_EVIL=1"},
		{PanelAuthToken: "tok\x00injected"},
		{PanelAuthToken: "tok", ACMECAURL: "https://ca/acme\nFOO=bar"},
	} {
		env, err := NewManagedMaterial(input).EnvContent()
		if err == nil {
			t.Fatalf("EnvContent(%+v) must reject control characters, got:\n%s", input, env)
		}
	}
	// A clean input still renders.
	env, err := NewManagedMaterial(Input{PanelAuthToken: "tok", PanelListen: "127.0.0.1:2096"}).EnvContent()
	if err != nil || !strings.Contains(env, "VEIL_LISTEN=127.0.0.1:2096\n") {
		t.Fatalf("clean EnvContent = %q, err %v", env, err)
	}
}

// TestManagedMaterialFilesFailsClosedOnEnvInjection ensures the write-out
// path propagates the EnvContent rejection instead of silently dropping the
// file or writing the injected content.
func TestManagedMaterialFilesFailsClosedOnEnvInjection(t *testing.T) {
	m := NewManagedMaterial(Input{
		Paths:          Paths{EtcDir: t.TempDir(), VarDir: t.TempDir(), SystemdDir: t.TempDir()},
		PanelAuthToken: "tok",
		PanelListen:    "127.0.0.1:2096\nMalicious=yes",
	})
	if _, err := m.Files(); err == nil {
		t.Fatal("Files must fail closed when veil.env cannot be rendered safely")
	}
}
