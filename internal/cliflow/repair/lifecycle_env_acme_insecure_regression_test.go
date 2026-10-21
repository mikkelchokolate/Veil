package repair

import (
	"os"
	"path/filepath"
	"testing"
)

// #1334 review: repair regenerates veil.env from repairLifecycleEnv, so a
// non-bool comparison here would silently persist VEIL_ACME_INSECURE=1 again.
func TestRepairLifecycleEnvParsesACMEInsecureAsBool(t *testing.T) {
	for _, tc := range []struct {
		name, persisted, env string
		setEnv               bool
		want                 bool
	}{
		{name: "persisted 1", persisted: "1", want: true},
		{name: "persisted 0", persisted: "0", want: false},
		{name: "persisted bogus", persisted: "bogus", want: false},
		{name: "env 0 beats persisted 1", persisted: "1", env: "0", setEnv: true, want: false},
		{name: "unset", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			etc := t.TempDir()
			content := ""
			if tc.persisted != "" {
				content = "VEIL_ACME_INSECURE=" + tc.persisted + "\n"
			}
			if err := os.WriteFile(filepath.Join(etc, "veil.env"), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("VEIL_ACME_INSECURE", tc.env)
			if !tc.setEnv {
				os.Unsetenv("VEIL_ACME_INSECURE")
			}
			if _, _, _, got := repairLifecycleEnv(Options{EtcDir: etc}); got != tc.want {
				t.Fatalf("insecure = %v, want %v", got, tc.want)
			}
		})
	}
}
