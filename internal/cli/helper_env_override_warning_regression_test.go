package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

var helperPolicyEnvNames = []string{
	"VEIL_ETC_DIR", "VEIL_VAR_DIR", "VEIL_STATE_PATH", "VEIL_KEY_PATH", "VEIL_APPLY_ROOT", "VEIL_LIVE_ROOT",
	"VEIL_BACKUP_PASSPHRASE", "VEIL_BACKUP_ROOT", "VEIL_BACKUP_SFTP_CONFIG", "VEIL_BACKUP_SFTP_STATUS",
	"VEIL_BACKUP_SFTP_KNOWN_HOSTS", "VEIL_BACKUP_SFTP_INSTALL_ID",
}

func runHelperServeForEnvWarning(t *testing.T, activation bool) (string, bool) {
	t.Helper()
	served := false
	stop := func() error { served = true; return errors.New("stop") }
	cmd := newHelperCommandWithDependencies(helperCommandDependencies{
		GOOS:           "linux",
		EffectiveUID:   func() int { return 0 },
		LookupUID:      func(string) (uint32, error) { return 4242, nil },
		Serve:          func(context.Context, string, privileged.PeerPolicy) error { return stop() },
		ServeActivated: func(context.Context, privileged.PeerPolicy) error { return stop() },
	})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetOut(&bytes.Buffer{})
	args := []string{"serve", "--socket", filepath.Join(t.TempDir(), "helper.sock")}
	if activation {
		args = []string{"serve", "--systemd-socket-activation"}
	}
	cmd.SetArgs(args)
	_ = cmd.Execute()
	return stderr.String(), served
}

// A root `veil helper serve` built its whole policy (state, key, backup root,
// SFTP config, ...) from VEIL_* in the process env without telling anyone, so
// `sudo -E veil helper serve` with a poisoned env silently retargeted key
// generation and backups (#1263). Every retargeting variable is now named on
// stderr before the helper starts serving, in both serve modes.
func TestHelperServeWarnsAboutEnvRetargetedPolicyPaths(t *testing.T) {
	for _, activation := range []bool{false, true} {
		for _, name := range helperPolicyEnvNames {
			for _, other := range helperPolicyEnvNames {
				t.Setenv(other, "")
			}
			value := filepath.Join(t.TempDir(), "attacker")
			t.Setenv(name, value)
			stderr, served := runHelperServeForEnvWarning(t, activation)
			if !served {
				t.Fatalf("activation=%v %s: helper did not reach serve", activation, name)
			}
			want := "warning: " + name + "=" + value + " from the environment overrides the packaged helper path"
			if !strings.Contains(stderr, want) {
				t.Fatalf("activation=%v: missing warning %q in stderr:\n%s", activation, want, stderr)
			}
		}
	}
}

func TestHelperServeDoesNotWarnForPackagedEnv(t *testing.T) {
	for _, name := range helperPolicyEnvNames {
		t.Setenv(name, "")
	}
	// The packaged helper unit's Environment= lines.
	t.Setenv("VEIL_STATE_PATH", "/var/lib/veil/state.json")
	t.Setenv("VEIL_KEY_PATH", "/etc/veil/state.key")
	t.Setenv("VEIL_APPLY_ROOT", "/var/lib/veil/staging")
	t.Setenv("VEIL_LIVE_ROOT", "/etc/veil/generated")
	for _, activation := range []bool{false, true} {
		stderr, served := runHelperServeForEnvWarning(t, activation)
		if !served {
			t.Fatal("helper did not reach serve")
		}
		if strings.Contains(stderr, "warning:") {
			t.Fatalf("activation=%v: packaged env must not warn, got:\n%s", activation, stderr)
		}
	}
}
