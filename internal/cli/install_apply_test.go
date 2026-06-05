package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/hostaccess"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestApplyRURecommendedInstallUsesDefaultBackupDirAndPrintsPanelCredentials(t *testing.T) {
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc
	oldPrepareHost := installPrepareHostFunc
	var gotPaths installer.ApplyPaths
	var gotActions []service.SystemdAction
	var gotHostPaths hostaccess.Paths
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		gotPaths = paths
		return installer.ApplyResult{BackupID: "backup-1", WrittenFiles: []string{"/etc/veil/veil.env"}}, nil
	}
	installSystemdRunFunc = func(actions []service.SystemdAction) error {
		gotActions = actions
		return nil
	}
	installExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	installPrepareHostFunc = func(paths hostaccess.Paths) error {
		gotHostPaths = paths
		return nil
	}
	t.Cleanup(func() {
		installApplyFunc = oldApply
		installSystemdRunFunc = oldSystemd
		installExecutableFunc = oldExecutable
		installPrepareHostFunc = oldPrepareHost
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	profile := installer.RURecommendedProfile{
		Domain:      "example.com",
		Username:    "veil",
		Password:    "test-password",
		WebBasePath: "/panel/",
	}

	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	if err := applyRURecommendedInstall(cmd, profile, ruRecommendedInstallOptions{EtcDir: tempEtc, VarDir: tempVar}); err != nil {
		t.Fatalf("applyRURecommendedInstall: %v", err)
	}
	if filepath.ToSlash(gotPaths.BackupDir) != filepath.ToSlash(filepath.Join(tempVar, "backups")) {
		t.Fatalf("BackupDir = %q, want %q", gotPaths.BackupDir, filepath.Join(tempVar, "backups"))
	}
	if filepath.ToSlash(gotPaths.SystemdDir) != "/etc/systemd/system" {
		t.Fatalf("SystemdDir = %q", gotPaths.SystemdDir)
	}
	if filepath.ToSlash(gotPaths.VeilBinary) != "/opt/veil/bin/veil" {
		t.Fatalf("VeilBinary = %q", gotPaths.VeilBinary)
	}
	if gotHostPaths.EtcDir != tempEtc || gotHostPaths.VarDir != tempVar {
		t.Fatalf("host paths=%+v", gotHostPaths)
	}
	if len(gotActions) == 0 || gotActions[0].Command != "systemctl" || gotActions[0].Args[0] != "daemon-reload" {
		t.Fatalf("systemd actions not run: %+v", gotActions)
	}
	if len(gotActions) < 2 || strings.Join(gotActions[1].Args, " ") != "enable veil-helper.socket" {
		t.Fatalf("helper socket must be enabled before panel: %+v", gotActions)
	}
	for _, want := range []string{"Written files:", "/etc/veil/veil.env", "Panel: https://example.com/panel/"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
	for _, unwanted := range []string{"NaiveProxy password:", "Hysteria2 password:"} {
		if strings.Contains(out.String(), unwanted) {
			t.Fatalf("output should not include protocol credential %q:\n%s", unwanted, out.String())
		}
	}

	// Verify key and state were created
	if _, err := os.Stat(filepath.Join(tempEtc, "state.key")); err != nil {
		t.Fatalf("state.key missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tempVar, "state.json")); err != nil {
		t.Fatalf("state.json missing: %v", err)
	}
}

func TestApplyRURecommendedInstallPreservesExistingState(t *testing.T) {
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc

	var gotProfile installer.RURecommendedProfile
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		gotProfile = profile
		return installer.ApplyResult{BackupID: "backup-2", WrittenFiles: []string{"/etc/veil/veil.env"}}, nil
	}
	installSystemdRunFunc = func(actions []service.SystemdAction) error {
		return nil
	}
	installExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	t.Cleanup(func() {
		installApplyFunc = oldApply
		installSystemdRunFunc = oldSystemd
		installExecutableFunc = oldExecutable
	})

	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	resolvedKeyPath := filepath.Join(tempEtc, "state.key")
	resolvedStatePath := filepath.Join(tempVar, "state.json")

	// Pre-create key and state
	key, err := secrets.LoadOrCreateKey(resolvedKeyPath)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}

	existingSnapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			PanelListen: "127.0.0.1:3000",
			WebBasePath: "/existing-path/",
		},
		Users: []model.User{
			{
				Username:     "existing_admin",
				PasswordHash: "fake-hash",
				Role:         "admin",
			},
		},
	}

	store := managementstate.NewStore(resolvedStatePath, cipher)
	if err := store.Save(existingSnapshot); err != nil {
		t.Fatalf("save existing state: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	// Call install with different parameters (should be overwritten by existing state)
	profile := installer.RURecommendedProfile{
		Domain:      "newexample.com",
		Username:    "random_generated_user",
		Password:    "random_generated_pass",
		WebBasePath: "/random-generated-path/",
	}

	if err := applyRURecommendedInstall(cmd, profile, ruRecommendedInstallOptions{EtcDir: tempEtc, VarDir: tempVar}); err != nil {
		t.Fatalf("applyRURecommendedInstall: %v", err)
	}

	// Verify that the Caddy/env generator received the preserved base path
	if gotProfile.WebBasePath != "/existing-path/" {
		t.Fatalf("expected preserved WebBasePath '/existing-path/', got %q", gotProfile.WebBasePath)
	}
	if gotProfile.Username != "existing_admin" {
		t.Fatalf("expected preserved Username 'existing_admin', got %q", gotProfile.Username)
	}

	got := out.String()
	if !strings.Contains(got, "Username: existing_admin") {
		t.Fatalf("expected output to contain existing_admin: %s", got)
	}
	if !strings.Contains(got, "Password: [preserved existing password]") {
		t.Fatalf("expected output to contain preserved password message: %s", got)
	}
}
