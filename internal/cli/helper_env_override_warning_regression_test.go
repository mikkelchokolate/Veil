package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// The variable list comes from the privileged table itself, so a knob added
// there is covered here without a second list to keep in sync.
var helperPolicyEnvNames = privileged.PolicyEnvKnobNames()

func clearHelperPolicyEnv(t *testing.T) {
	t.Helper()
	for _, name := range helperPolicyEnvNames {
		t.Setenv(name, "")
	}
}

func helperEnvWarningLines(stderr string) []string {
	var lines []string
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "warning: ") {
			lines = append(lines, line)
		}
	}
	return lines
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
			clearHelperPolicyEnv(t)
			value := filepath.Join(t.TempDir(), "attacker")
			t.Setenv(name, value)
			stderr, served := runHelperServeForEnvWarning(t, activation)
			if !served {
				t.Fatalf("activation=%v %s: helper did not reach serve", activation, name)
			}
			want := "warning: " + name + "=" + strconv.Quote(value) + " from the environment overrides the packaged helper path \""
			if !strings.Contains(stderr, want) {
				t.Fatalf("activation=%v: missing warning %q in stderr:\n%s", activation, want, stderr)
			}
		}
	}
}

func TestHelperServeDoesNotWarnForPackagedEnv(t *testing.T) {
	clearHelperPolicyEnv(t)
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

// Guards against an empty or truncated knob list making the per-variable
// loops above pass vacuously; the exact table is pinned in privileged by
// TestPolicyEnvKnobsCoverEveryDefaultPolicyEnvRead.
func TestHelperServeEnvWarningKnobListIsNotTruncated(t *testing.T) {
	if len(helperPolicyEnvNames) < 12 {
		t.Fatalf("expected the full privileged knob table, got %v", helperPolicyEnvNames)
	}
}

// An env value with an embedded newline must not forge a separate journal
// line inside the warning the operator is meant to trust (#1390 review).
func TestHelperServeEnvWarningQuotesValues(t *testing.T) {
	clearHelperPolicyEnv(t)
	t.Setenv("VEIL_BACKUP_ROOT", "/tmp/evil\nveil: helper policy verified, no overrides")
	stderr, served := runHelperServeForEnvWarning(t, false)
	if !served {
		t.Fatal("helper did not reach serve")
	}
	if strings.Contains(stderr, "\nveil: helper policy verified") {
		t.Fatalf("raw newline from the env reached stderr:\n%s", stderr)
	}
	want := `warning: VEIL_BACKUP_ROOT="/tmp/evil\nveil: helper policy verified, no overrides" from the environment overrides the packaged helper path "/var/lib/veil/backups"; the root helper trusts it`
	if lines := helperEnvWarningLines(stderr); len(lines) != 1 || lines[0] != want {
		t.Fatalf("want exactly %q, got %q", want, lines)
	}
}

// VEIL_BACKUP_PASSPHRASE is a path, but an operator who exports the
// passphrase itself must not see it echoed into the journal (#1390 review).
func TestHelperServeEnvWarningRedactsNonPathPassphraseValue(t *testing.T) {
	clearHelperPolicyEnv(t)
	secret := "correct horse battery staple"
	t.Setenv("VEIL_BACKUP_PASSPHRASE", secret)
	stderr, served := runHelperServeForEnvWarning(t, false)
	if !served {
		t.Fatal("helper did not reach serve")
	}
	if strings.Contains(stderr, secret) || strings.Contains(stderr, "horse") {
		t.Fatalf("passphrase-like value echoed to stderr:\n%s", stderr)
	}
	want := `warning: VEIL_BACKUP_PASSPHRASE=<redacted: not an absolute path without spaces> from the environment overrides the packaged helper path "/etc/veil/backup.passphrase"; the root helper trusts it`
	if lines := helperEnvWarningLines(stderr); len(lines) != 1 || lines[0] != want {
		t.Fatalf("want exactly %q, got %q", want, lines)
	}
}

// A helper unit rendered for a custom --etc-dir exports VEIL_KEY_PATH and
// VEIL_LIVE_ROOT under that root (and the packaged var-dir paths): each start
// must warn about exactly those two, nothing else.
func TestHelperServeCustomEtcDirUnitWarnsExactlyForItsRetargetedPaths(t *testing.T) {
	clearHelperPolicyEnv(t)
	t.Setenv("VEIL_STATE_PATH", "/var/lib/veil/state.json")
	t.Setenv("VEIL_KEY_PATH", "/opt/veil/etc/state.key")
	t.Setenv("VEIL_APPLY_ROOT", "/var/lib/veil/staging")
	t.Setenv("VEIL_LIVE_ROOT", "/opt/veil/etc/generated")
	stderr, served := runHelperServeForEnvWarning(t, true)
	if !served {
		t.Fatal("helper did not reach serve")
	}
	want := []string{
		`warning: VEIL_KEY_PATH="/opt/veil/etc/state.key" from the environment overrides the packaged helper path "/etc/veil/state.key"; the root helper trusts it`,
		`warning: VEIL_LIVE_ROOT="/opt/veil/etc/generated" from the environment overrides the packaged helper path "/etc/veil/generated"; the root helper trusts it`,
	}
	if got := helperEnvWarningLines(stderr); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("want %q, got %q", want, got)
	}
}
