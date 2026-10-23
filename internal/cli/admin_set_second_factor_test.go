package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
)

// enrolledAdmin is an account with every second-factor field populated so a
// test can tell which ones `admin set` kept, cleared or rewound (#1447).
func enrolledAdmin(username, role string) model.User {
	return model.User{
		Username: username, PasswordHash: "old", Role: role, Locale: "ru",
		TOTPEnabled: true, TOTPSecret: "JBSWY3DPEHPK3PXP", TOTPPendingSecret: "KRSXG5CTMVRXEZLU",
		TOTPRecoveryHashes: []string{"aa", "bb"}, TOTPLastStep: 58000000,
		Passkeys: []model.Passkey{{ID: "cred1", Name: "yubikey", PublicKey: "AAAA", SignCount: 7}},
	}
}

func runAdminSet(t *testing.T, statePath, keyPath string, extra ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := newFastRootCommand("test")
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	args := append([]string{"admin", "set", "--password", "a-long-secure-password",
		"--state", statePath, "--key-path", keyPath}, extra...)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func requireSecondFactorsKept(t *testing.T, got, want model.User) {
	t.Helper()
	if !got.TOTPEnabled || got.TOTPSecret != want.TOTPSecret || got.TOTPPendingSecret != want.TOTPPendingSecret {
		t.Errorf("TOTP factor not kept: enabled=%v secret=%q pending=%q", got.TOTPEnabled, got.TOTPSecret, got.TOTPPendingSecret)
	}
	if strings.Join(got.TOTPRecoveryHashes, ",") != strings.Join(want.TOTPRecoveryHashes, ",") {
		t.Errorf("recovery hashes not kept: %v", got.TOTPRecoveryHashes)
	}
	if got.TOTPLastStep != want.TOTPLastStep {
		t.Errorf("replay watermark rewound: %d, want %d", got.TOTPLastStep, want.TOTPLastStep)
	}
	if len(got.Passkeys) != 1 || got.Passkeys[0].ID != "cred1" || got.Passkeys[0].SignCount != 7 {
		t.Errorf("passkeys not kept: %+v", got.Passkeys)
	}
}

func TestAdminSetPasswordChangeKeepsSecondFactors(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	keyPath := filepath.Join(t.TempDir(), "state.key")
	before := enrolledAdmin("admin", "admin")
	writeAdminState(t, statePath, keyPath, []model.User{before})

	out, err := runAdminSet(t, statePath, keyPath, "--username", "admin")
	if err != nil {
		t.Fatal(err)
	}
	after := loadAdminUsers(t, statePath, keyPath)[0]
	if after.PasswordHash == "old" || after.PasswordHash == "" {
		t.Fatalf("password was not changed: %q", after.PasswordHash)
	}
	if after.Role != "admin" || after.Locale != "ru" {
		t.Fatalf("role/locale changed: %+v", after)
	}
	requireSecondFactorsKept(t, after, before)
	if !strings.Contains(out, "Second factors: kept (TOTP, pending TOTP enrollment, 2 recovery code(s), 1 passkey(s)); pass --clear-2fa to remove them\n") {
		t.Errorf("output does not say the second factors were kept:\n%s", out)
	}
}

func TestAdminSetRoleChangeKeepsSecondFactors(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	keyPath := filepath.Join(t.TempDir(), "state.key")
	before := enrolledAdmin("ops", "admin")
	writeAdminState(t, statePath, keyPath, []model.User{
		{Username: "admin", PasswordHash: "x", Role: "admin"},
		before,
	})

	if _, err := runAdminSet(t, statePath, keyPath, "--username", "ops", "--role", "viewer"); err != nil {
		t.Fatal(err)
	}
	after := loadAdminUsers(t, statePath, keyPath)[1]
	if after.Role != "viewer" {
		t.Fatalf("role not changed: %q", after.Role)
	}
	requireSecondFactorsKept(t, after, before)
}

func TestAdminSetClear2FARemovesFactorsButKeepsWatermark(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	keyPath := filepath.Join(t.TempDir(), "state.key")
	before := enrolledAdmin("admin", "admin")
	writeAdminState(t, statePath, keyPath, []model.User{before})

	out, err := runAdminSet(t, statePath, keyPath, "--username", "admin", "--clear-2fa")
	if err != nil {
		t.Fatal(err)
	}
	after := loadAdminUsers(t, statePath, keyPath)[0]
	if after.TOTPEnabled || after.TOTPSecret != "" || after.TOTPPendingSecret != "" || len(after.TOTPRecoveryHashes) != 0 {
		t.Errorf("TOTP not cleared: %+v", after)
	}
	if len(after.Passkeys) != 0 {
		t.Errorf("passkeys not cleared: %+v", after.Passkeys)
	}
	if after.TOTPLastStep != before.TOTPLastStep {
		t.Errorf("--clear-2fa rewound the replay watermark: %d, want %d", after.TOTPLastStep, before.TOTPLastStep)
	}
	if !strings.Contains(out, "Second factors: cleared (TOTP, pending TOTP enrollment, 2 recovery code(s), 1 passkey(s));") {
		t.Errorf("output does not say the second factors were cleared:\n%s", out)
	}
}

func TestAdminSetClear2FARefusesUnknownUser(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	keyPath := filepath.Join(t.TempDir(), "state.key")
	writeAdminState(t, statePath, keyPath, []model.User{enrolledAdmin("admin", "admin")})

	_, err := runAdminSet(t, statePath, keyPath, "--username", "admni", "--clear-2fa")
	if err == nil || !strings.Contains(err.Error(), "--clear-2fa only applies to an existing user") {
		t.Fatalf("expected --clear-2fa refusal for an unknown user, got %v", err)
	}
	if strings.Contains(err.Error(), "save state") {
		t.Errorf("refusal reported as a save failure: %v", err)
	}
	users := loadAdminUsers(t, statePath, keyPath)
	if len(users) != 1 || users[0].PasswordHash != "old" || !users[0].TOTPEnabled {
		t.Fatalf("state changed by a refused --clear-2fa: %+v", users)
	}
}

func TestAdminSetExistingUserWithoutFactorsReport(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra []string
		want  string
	}{
		{name: "no flag prints no factor line", want: ""},
		{name: "clear-2fa reports none to clear", extra: []string{"--clear-2fa"}, want: "Second factors: none to clear\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "state.json")
			keyPath := filepath.Join(t.TempDir(), "state.key")
			writeAdminState(t, statePath, keyPath, []model.User{{Username: "admin", PasswordHash: "old", Role: "admin"}})

			out, err := runAdminSet(t, statePath, keyPath, append([]string{"--username", "admin"}, tc.extra...)...)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if strings.Contains(out, "Second factors:") {
					t.Errorf("unexpected factor line:\n%s", out)
				}
			} else if !strings.Contains(out, tc.want) {
				t.Errorf("output lacks %q:\n%s", tc.want, out)
			}
		})
	}
}

func TestAdminSetNewUserOutputHasNoSecondFactorLine(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	keyPath := filepath.Join(t.TempDir(), "state.key")
	writeAdminState(t, statePath, keyPath, []model.User{{Username: "admin", PasswordHash: "x", Role: "admin"}})

	out, err := runAdminSet(t, statePath, keyPath, "--username", "newbie", "--role", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	users := loadAdminUsers(t, statePath, keyPath)
	if len(users) != 2 || users[1].Username != "newbie" || users[1].HasSecondFactor() {
		t.Fatalf("unexpected users: %+v", users)
	}
	if strings.Contains(out, "Second factors:") {
		t.Errorf("new user output mentions second factors:\n%s", out)
	}
}
