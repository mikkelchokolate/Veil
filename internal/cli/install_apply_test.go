package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/hostaccess"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/service"
)

func withMockedInstallRuntimes(t *testing.T) {
	old := installRuntimesFunc
	installRuntimesFunc = func(*cobra.Command, ruRecommendedInstallOptions) {}
	t.Cleanup(func() { installRuntimesFunc = old })
}

func TestApplyRURecommendedInstallUsesDefaultBackupDirAndPrintsPanelCredentials(t *testing.T) {
	withMockedInstallRuntimes(t)
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

	key, err := secrets.LoadOrCreateKey(filepath.Join(tempEtc, "state.key"))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	snapshot, ok, err := managementstate.NewStore(filepath.Join(tempVar, "state.json"), cipher).Load()
	if err != nil || !ok {
		t.Fatalf("load state: ok=%v err=%v", ok, err)
	}
	if !snapshot.Setup.Completed || len(snapshot.Users) != 1 || snapshot.Users[0].Role != "admin" {
		t.Fatalf("fresh install must persist completed setup with admin, got setup=%+v users=%+v", snapshot.Setup, snapshot.Users)
	}
}

func TestApplyRURecommendedInstallFailsWhenExistingStateUnreadable(t *testing.T) {
	withMockedInstallRuntimes(t)
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc
	oldPrepareHost := installPrepareHostFunc
	applyCalled := false
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		applyCalled = true
		return installer.ApplyResult{}, nil
	}
	installSystemdRunFunc = func(actions []service.SystemdAction) error { return nil }
	installExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	installPrepareHostFunc = func(paths hostaccess.Paths) error { return nil }
	t.Cleanup(func() {
		installApplyFunc = oldApply
		installSystemdRunFunc = oldSystemd
		installExecutableFunc = oldExecutable
		installPrepareHostFunc = oldPrepareHost
	})

	tempEtc := t.TempDir()
	tempVar := t.TempDir()
	resolvedKeyPath := filepath.Join(tempEtc, "state.key")
	resolvedStatePath := filepath.Join(tempVar, "state.json")

	// Persist state encrypted under one key...
	keyA, err := secrets.LoadOrCreateKey(resolvedKeyPath)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	cipherA, err := secrets.NewCipher(*keyA)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	if err := managementstate.NewStore(resolvedStatePath, cipherA).Save(model.ManagementSnapshot{
		// An encrypted secret field is what makes a wrong key fail to decrypt.
		Settings: model.Settings{WebBasePath: "/old/", Hysteria2Password: "super-secret"},
		Users:    []model.User{{Username: "old_admin", PasswordHash: "hash", Role: "admin"}},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	// ...then swap in a different key, simulating /etc/veil being recreated while
	// /var/lib/veil/state.json survived. The new key cannot decrypt the old state.
	var keyB [secrets.KeySize]byte
	for i := range keyB {
		keyB[i] = byte(i + 1)
	}
	if err := os.WriteFile(resolvedKeyPath, keyB[:], 0o600); err != nil {
		t.Fatalf("write mismatched key: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	profile := installer.RURecommendedProfile{Username: "fresh", Password: "fresh", WebBasePath: "/fresh/"}

	err = applyRURecommendedInstall(cmd, profile, ruRecommendedInstallOptions{EtcDir: tempEtc, VarDir: tempVar})
	if err == nil {
		t.Fatal("expected an error when existing state cannot be decrypted")
	}
	if !strings.Contains(err.Error(), "could not read it with the encryption key") {
		t.Fatalf("error should guide recovery, got: %v", err)
	}
	if applyCalled {
		t.Fatal("install must not write generated files when existing state is unreadable")
	}
}

func TestShouldPrepareInstallHostOnlyForCanonicalPaths(t *testing.T) {
	if !shouldPrepareInstallHost(defaultSystemdDir) {
		t.Fatal("canonical native install must prepare the service account and permissions")
	}
	if shouldPrepareInstallHost(t.TempDir()) {
		t.Fatal("staging install must not mutate current-host accounts or ownership")
	}
}

func TestApplyRURecommendedInstallPreservesExistingState(t *testing.T) {
	withMockedInstallRuntimes(t)
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

func TestApplyRURecommendedInstallAppliesFirewallRules(t *testing.T) {
	withMockedInstallRuntimes(t)
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc
	oldPrepareHost := installPrepareHostFunc
	oldFirewall := installFirewallApplyFunc

	var gotRules []firewall.Rule
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		return installer.ApplyResult{BackupID: "backup-fw", WrittenFiles: []string{"/etc/veil/veil.env"}}, nil
	}
	installSystemdRunFunc = func(actions []service.SystemdAction) error { return nil }
	installExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	installPrepareHostFunc = func(paths hostaccess.Paths) error { return nil }
	installFirewallApplyFunc = func(rules []firewall.Rule) error {
		gotRules = rules
		return nil
	}
	t.Cleanup(func() {
		installApplyFunc = oldApply
		installSystemdRunFunc = oldSystemd
		installExecutableFunc = oldExecutable
		installPrepareHostFunc = oldPrepareHost
		installFirewallApplyFunc = oldFirewall
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	profile := installer.RURecommendedProfile{
		Domain:      "example.com",
		Username:    "veil",
		Password:    "test-password",
		WebBasePath: "/panel/",
		PanelListen: "0.0.0.0:3000",
	}

	if err := applyRURecommendedInstall(cmd, profile, ruRecommendedInstallOptions{EtcDir: tempEtc, VarDir: tempVar, PanelPort: 3000}); err != nil {
		t.Fatalf("applyRURecommendedInstall: %v", err)
	}

	if len(gotRules) == 0 {
		t.Fatal("expected firewall rules to be applied")
	}
	foundPanel := false
	for _, r := range gotRules {
		if len(r.Args) >= 1 && r.Args[0] == "allow" && strings.Contains(r.Args[1], "3000/tcp") {
			foundPanel = true
			break
		}
	}
	if !foundPanel {
		t.Fatalf("expected panel port firewall rule, got %+v", gotRules)
	}
}

func TestApplyRURecommendedInstallDirectIssuesLEIPCert(t *testing.T) {
	withMockedInstallRuntimes(t)
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc
	oldPrepareHost := installPrepareHostFunc
	oldIssue := leIPCertIssueFunc

	var gotProfile installer.RURecommendedProfile
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		gotProfile = profile
		return installer.ApplyResult{BackupID: "backup-le", WrittenFiles: []string{"/etc/veil/veil.env"}}, nil
	}
	installSystemdRunFunc = func(actions []service.SystemdAction) error { return nil }
	installExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	installPrepareHostFunc = func(paths hostaccess.Paths) error { return nil }
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		if err := os.WriteFile(opts.CertPath, []byte("LE-CERT"), 0o644); err != nil {
			t.Fatalf("write fake cert: %v", err)
		}
		if err := os.WriteFile(opts.KeyPath, []byte("LE-KEY"), 0o640); err != nil {
			t.Fatalf("write fake key: %v", err)
		}
		return acmeip.IssuedCert{CertPath: opts.CertPath, KeyPath: opts.KeyPath}, nil
	}
	t.Cleanup(func() {
		installApplyFunc = oldApply
		installSystemdRunFunc = oldSystemd
		installExecutableFunc = oldExecutable
		installPrepareHostFunc = oldPrepareHost
		leIPCertIssueFunc = oldIssue
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	profile := installer.RURecommendedProfile{
		Username:    "veil",
		Password:    "test-password",
		WebBasePath: "/panel/",
		PanelListen: "0.0.0.0:3000",
		PanelAccess: "direct",
	}

	if err := applyRURecommendedInstall(cmd, profile, ruRecommendedInstallOptions{EtcDir: tempEtc, VarDir: tempVar, PanelAccess: "direct", LEIPCert: true, PublicIP: "127.0.0.1"}); err != nil {
		t.Fatalf("applyRURecommendedInstall: %v", err)
	}
	if gotProfile.PanelTLSCertPEM != "LE-CERT" {
		t.Fatalf("expected issued cert in profile, got %q", gotProfile.PanelTLSCertPEM)
	}
	if gotProfile.PanelTLSKeyPEM != "LE-KEY" {
		t.Fatalf("expected issued key in profile, got %q", gotProfile.PanelTLSKeyPEM)
	}
}

func TestApplyRURecommendedInstallDirectFallsBackToSelfSignedOnLEIPCertFailure(t *testing.T) {
	withMockedInstallRuntimes(t)
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc
	oldPrepareHost := installPrepareHostFunc
	oldIssue := leIPCertIssueFunc

	var gotProfile installer.RURecommendedProfile
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		gotProfile = profile
		return installer.ApplyResult{BackupID: "backup-fallback", WrittenFiles: []string{"/etc/veil/veil.env"}}, nil
	}
	installSystemdRunFunc = func(actions []service.SystemdAction) error { return nil }
	installExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	installPrepareHostFunc = func(paths hostaccess.Paths) error { return nil }
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		return acmeip.IssuedCert{}, errors.New("port 80 blocked")
	}
	t.Cleanup(func() {
		installApplyFunc = oldApply
		installSystemdRunFunc = oldSystemd
		installExecutableFunc = oldExecutable
		installPrepareHostFunc = oldPrepareHost
		leIPCertIssueFunc = oldIssue
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	profile := installer.RURecommendedProfile{
		Username:        "veil",
		Password:        "test-password",
		WebBasePath:     "/panel/",
		PanelListen:     "0.0.0.0:3000",
		PanelAccess:     "direct",
		PanelTLSEnabled: true,
		PanelTLSCertPEM: "SELF-SIGNED-CERT",
		PanelTLSKeyPEM:  "SELF-SIGNED-KEY",
	}

	if err := applyRURecommendedInstall(cmd, profile, ruRecommendedInstallOptions{EtcDir: tempEtc, VarDir: tempVar, PanelAccess: "direct", LEIPCert: true, PublicIP: "127.0.0.1"}); err != nil {
		t.Fatalf("applyRURecommendedInstall: %v", err)
	}
	if gotProfile.PanelTLSCertPEM != "SELF-SIGNED-CERT" {
		t.Fatalf("expected self-signed cert to be preserved, got %q", gotProfile.PanelTLSCertPEM)
	}
	if !strings.Contains(out.String(), "could not obtain Let's Encrypt IP certificate") {
		t.Fatalf("expected warning about LE cert failure, got:\n%s", out.String())
	}
}

func TestApplyRURecommendedInstallDirectOpensACMEPort(t *testing.T) {
	withMockedInstallRuntimes(t)
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc
	oldPrepareHost := installPrepareHostFunc
	oldFirewall := installFirewallApplyFunc
	oldIssue := leIPCertIssueFunc

	var gotRules []firewall.Rule
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		return installer.ApplyResult{BackupID: "backup-acme", WrittenFiles: []string{"/etc/veil/veil.env"}}, nil
	}
	installSystemdRunFunc = func(actions []service.SystemdAction) error { return nil }
	installExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	installPrepareHostFunc = func(paths hostaccess.Paths) error { return nil }
	installFirewallApplyFunc = func(rules []firewall.Rule) error {
		gotRules = rules
		return nil
	}
	leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
		return acmeip.IssuedCert{CertPath: opts.CertPath, KeyPath: opts.KeyPath}, nil
	}
	t.Cleanup(func() {
		installApplyFunc = oldApply
		installSystemdRunFunc = oldSystemd
		installExecutableFunc = oldExecutable
		installPrepareHostFunc = oldPrepareHost
		installFirewallApplyFunc = oldFirewall
		leIPCertIssueFunc = oldIssue
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	tempEtc := t.TempDir()
	tempVar := t.TempDir()

	profile := installer.RURecommendedProfile{
		Username:    "veil",
		Password:    "test-password",
		WebBasePath: "/panel/",
		PanelListen: "0.0.0.0:3000",
		PanelAccess: "direct",
	}

	if err := applyRURecommendedInstall(cmd, profile, ruRecommendedInstallOptions{EtcDir: tempEtc, VarDir: tempVar, PanelAccess: "direct", PanelPort: 3000, LEIPCert: true, LEIPCertPort: 80, PublicIP: "127.0.0.1"}); err != nil {
		t.Fatalf("applyRURecommendedInstall: %v", err)
	}

	foundACME := false
	for _, r := range gotRules {
		if len(r.Args) >= 1 && r.Args[0] == "allow" && strings.Contains(r.Args[1], "80/tcp") {
			foundACME = true
			break
		}
	}
	if !foundACME {
		t.Fatalf("expected ACME port 80 firewall rule, got %+v", gotRules)
	}
}

func TestApplyRURecommendedInstallDirectFillsDomainWithPublicIP(t *testing.T) {
	withMockedInstallRuntimes(t)
	oldApply := installApplyFunc
	oldSystemd := installSystemdRunFunc
	oldExecutable := installExecutableFunc
	oldPrepareHost := installPrepareHostFunc
	var gotProfile installer.RURecommendedProfile
	installApplyFunc = func(profile installer.RURecommendedProfile, paths installer.ApplyPaths) (installer.ApplyResult, error) {
		gotProfile = profile
		return installer.ApplyResult{BackupID: "backup-domain", WrittenFiles: []string{"/etc/veil/veil.env"}}, nil
	}
	installSystemdRunFunc = func(actions []service.SystemdAction) error { return nil }
	installExecutableFunc = func() (string, error) { return "/opt/veil/bin/veil", nil }
	installPrepareHostFunc = func(paths hostaccess.Paths) error { return nil }
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

	tempEtc := t.TempDir()
	tempVar := t.TempDir()
	profile := installer.RURecommendedProfile{
		Username:    "veil",
		Password:    "test-password",
		WebBasePath: "/panel/",
		PanelListen: "0.0.0.0:3000",
		PanelAccess: "direct",
	}

	if err := applyRURecommendedInstall(cmd, profile, ruRecommendedInstallOptions{EtcDir: tempEtc, VarDir: tempVar, PanelAccess: "direct", PublicIP: "127.0.0.1"}); err != nil {
		t.Fatalf("applyRURecommendedInstall: %v", err)
	}
	if gotProfile.Domain != "127.0.0.1" {
		t.Fatalf("expected profile domain to be filled with public IP, got %q", gotProfile.Domain)
	}

	key, err := secrets.LoadOrCreateKey(filepath.Join(tempEtc, "state.key"))
	if err != nil {
		t.Fatalf("load state key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	snapshot, ok, err := managementstate.NewStore(filepath.Join(tempVar, "state.json"), cipher).Load()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if !ok {
		t.Fatal("state was not created")
	}
	if snapshot.Settings.Domain != "127.0.0.1" {
		t.Fatalf("expected state domain to be filled with public IP, got %q", snapshot.Settings.Domain)
	}
}
