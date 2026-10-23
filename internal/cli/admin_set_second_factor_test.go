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
	if !strings.Contains(out, "Second factors: kept") {
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
	if !strings.Contains(out, "Second factors: cleared") {
		t.Errorf("output does not say the second factors were cleared:\n%s", out)
	}
}

func TestAdminSetNewUserOutputHasNoSecondFactorLine(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	keyPath := filepath.Join(t.TempDir(), "state.key")
	writeAdminState(t, statePath, keyPath, []model.User{{Username: "admin", PasswordHash: "x", Role: "admin"}})

	out, err := runAdminSet(t, statePath, keyPath, "--username", "newbie", "--role", "viewer", "--clear-2fa")
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
