package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/installer"
)

// plantPATHShims puts executable shims for names into a directory prepended
// to PATH. Each shim records that it ran by creating <dir>/<name>.ran.
func plantPATHShims(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		script := "#!/bin/sh\n: > '" + filepath.Join(dir, name+".ran") + "'\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func assertShimNotRun(t *testing.T, dir, name string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, name+".ran")); err == nil {
		t.Fatalf("PATH shim %q was executed by a privileged Veil path", name)
	}
}

// #1262: a poisoned PATH during install must not be resolved into the caddy
// path persisted into veil-caddy.service ExecStart.
func TestInstallCaddyBinaryIsNotResolvedFromAmbientPATH(t *testing.T) {
	dir := plantPATHShims(t, "caddy", "ufw")
	shim := filepath.Join(dir, "caddy")
	profile := installer.RURecommendedProfile{InstallPanelCaddy: true}

	if got := installPlanCaddyBinary(profile); got == shim {
		t.Fatalf("installPlanCaddyBinary resolved the PATH shim %q", got)
	}
	if got, err := validateInstallRuntimePrerequisites(profile); err == nil && got == shim {
		t.Fatalf("validateInstallRuntimePrerequisites resolved the PATH shim %q", got)
	}
	if got, err := execLookPath("caddy"); err == nil && got == shim {
		t.Fatalf("execLookPath resolved the PATH shim %q", got)
	}
	if got, err := execLookPath("ufw"); err == nil && filepath.Dir(got) == dir {
		t.Fatalf("execLookPath resolved the PATH shim %q", got)
	}
	plan, err := buildInstallPlan(profile, ruRecommendedInstallOptions{})
	if err != nil {
		t.Fatalf("buildInstallPlan: %v", err)
	}
	if plan.CaddyBuild.BinaryPath == shim {
		t.Fatalf("install plan persisted the PATH shim as the caddy binary: %q", plan.CaddyBuild.BinaryPath)
	}
}

// #1262: root-run systemctl/package-manager helpers exec by trusted absolute
// path, never via a shim earlier in PATH.
func TestPrivilegedHelpersDoNotExecAmbientPATHShims(t *testing.T) {
	dir := plantPATHShims(t, "systemctl", "apt-get")

	_ = adminSystemctlRun("is-active", "--quiet", "veil-test-nonexistent-1262.service")
	assertShimNotRun(t, dir, "systemctl")
	_ = backupSystemctlRun("is-active", "--quiet", "veil-test-nonexistent-1262.service")
	assertShimNotRun(t, dir, "systemctl")
	_ = activeForeignFirewallFunc(context.Background())
	assertShimNotRun(t, dir, "systemctl")
	_ = installCaddyUnitActiveFunc(context.Background())
	assertShimNotRun(t, dir, "systemctl")
	_ = provisionUFWCommandFunc(context.Background(), "apt-get", "--version")
	assertShimNotRun(t, dir, "apt-get")
}
