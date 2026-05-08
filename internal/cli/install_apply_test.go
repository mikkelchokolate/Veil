package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
	"github.com/veil-panel/veil/internal/service"
)

func TestApplyRURecommendedInstallUsesDefaultBackupDirAndPrintsPanelCredentials(t *testing.T) {
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc
	var gotPaths installer.ApplyPaths
	var gotActions []service.SystemdAction
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		gotPaths = paths
		return installer.ApplyResult{BackupID: "backup-1", WrittenFiles: []string{"/etc/veil/veil.env"}}, nil
	}
	installSystemdRunFunc = func(actions []service.SystemdAction) error {
		gotActions = actions
		return nil
	}
	installExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	t.Cleanup(func() {
		installApplyFunc = oldApply
		installSystemdRunFunc = oldSystemd
		installExecutableFunc = oldExecutable
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	profile := installer.RURecommendedProfile{
		Domain:      "example.com",
		Username:    "veil",
		WebBasePath: "/panel/",
	}

	if err := applyRURecommendedInstall(cmd, profile, ruRecommendedInstallOptions{EtcDir: "/etc/veil", VarDir: "/var/lib/veil"}); err != nil {
		t.Fatalf("applyRURecommendedInstall: %v", err)
	}
	if gotPaths.BackupDir != "/var/lib/veil/backups" {
		t.Fatalf("BackupDir = %q", gotPaths.BackupDir)
	}
	if gotPaths.SystemdDir != "/etc/systemd/system" {
		t.Fatalf("SystemdDir = %q", gotPaths.SystemdDir)
	}
	if gotPaths.VeilBinary != "/opt/veil/bin/veil" {
		t.Fatalf("VeilBinary = %q", gotPaths.VeilBinary)
	}
	if len(gotActions) == 0 || gotActions[0].Command != "systemctl" || gotActions[0].Args[0] != "daemon-reload" {
		t.Fatalf("systemd actions not run: %+v", gotActions)
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
}
