package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/installer"
	"github.com/veil-panel/veil/internal/service"
)

func TestApplyRURecommendedInstallUsesDefaultBackupDirAndPrintsCredentials(t *testing.T) {
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
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
	t.Cleanup(func() {
		installApplyFunc = oldApply
		installSystemdRunFunc = oldSystemd
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	profile := installer.RURecommendedProfile{
		Domain:            "example.com",
		Username:          "veil",
		WebBasePath:       "/panel/",
		InstallNaive:      true,
		NaivePassword:     "naive-secret",
		InstallHysteria2:  true,
		Hysteria2Password: "hy2-secret",
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
	if len(gotActions) == 0 || gotActions[0].Command != "systemctl" || gotActions[0].Args[0] != "daemon-reload" {
		t.Fatalf("systemd actions not run: %+v", gotActions)
	}
	for _, want := range []string{"Written files:", "/etc/veil/veil.env", "Panel: https://example.com/panel/", "NaiveProxy password: naive-secret", "Hysteria2 password: hy2-secret"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}
