package panelmaterial

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/veilenv"
)

// TestEnvContentRecordsACustomSystemdDir locks the persistence half of #1484:
// a custom --systemd-dir install has to record where its units live, while the
// packaged default stays implicit so unchanged installs keep their environment.
func TestEnvContentRecordsACustomSystemdDir(t *testing.T) {
	custom := "/opt/veil/systemd"
	env, err := NewManagedMaterial(Input{
		Paths:          Paths{EtcDir: "/opt/veil/etc", VarDir: "/opt/veil/var", SystemdDir: custom},
		PanelAuthToken: "token",
	}).EnvContent()
	if err != nil {
		t.Fatalf("EnvContent: %v", err)
	}
	if got := veilenv.Value([]byte(env), veilenv.SystemdDirKey); got != custom {
		t.Fatalf("%s = %q, want %q\n%s", veilenv.SystemdDirKey, got, custom, env)
	}

	env, err = NewManagedMaterial(Input{
		Paths:          Paths{EtcDir: "/etc/veil", VarDir: "/var/lib/veil", SystemdDir: veilenv.DefaultSystemdDir},
		PanelAuthToken: "token",
	}).EnvContent()
	if err != nil {
		t.Fatalf("EnvContent: %v", err)
	}
	if strings.Contains(env, veilenv.SystemdDirKey) {
		t.Fatalf("the packaged unit directory must stay implicit:\n%s", env)
	}
}
