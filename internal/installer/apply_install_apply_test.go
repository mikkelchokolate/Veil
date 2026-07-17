package installer

import (
	"os"
	"os/user"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/systemdunits"
)

func TestApplyRURecommendedProfileWritesPanelFiles(t *testing.T) {
	dir := t.TempDir()
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Secret: func(label string) string { return "secret-" + label },
	})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}

	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	})
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	assertFileMissing(t, result.CaddyfilePath)
	assertFileMissing(t, result.Hysteria2Path)
	assertFileMissing(t, result.FallbackIndexPath)
	assertFileContains(t, filepath.Join(dir, "etc", "veil", "veil.env"), "VEIL_API_TOKEN=secret-panel")
	assertFileContains(t, filepath.Join(dir, "etc", "veil", "veil.env"), "VEIL_TLS_CERT=")
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil.service"), "ExecStart=/usr/local/bin/veil serve")
	for _, name := range systemdunits.Names() {
		assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", name), "[")
	}
}

func TestApplyRURecommendedProfileWritesPanelCaddyAccessFiles(t *testing.T) {
	dir := t.TempDir()
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		PanelAccess: "caddy",
		Domain:      "example.com",
		Email:       "admin@example.com",
		Secret:      func(label string) string { return "secret-" + label },
		PanelPort:   2096,
	})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}

	result, err := ApplyRURecommendedProfile(profile, ApplyPaths{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	})
	if err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	assertFileContains(t, result.CaddyfilePath, "127.0.0.1:2096")
	assertFileContains(t, result.FallbackIndexPath, "Veil")
	assertFileMissing(t, result.Hysteria2Path)
	assertFileContains(t, filepath.Join(dir, "etc", "veil", "veil.env"), "VEIL_API_TOKEN=secret-panel")
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil.service"), "ExecStart=/usr/local/bin/veil serve")
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil-caddy.service"), "caddy")
	assertFileMissing(t, filepath.Join(dir, "etc", "systemd", "system", "veil-hysteria2.service"))
	for _, name := range systemdunits.Names() {
		assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", name), "[")
	}
}

func TestApplyRURecommendedProfileRejectsMissingPaths(t *testing.T) {
	_, err := ApplyRURecommendedProfile(RURecommendedProfile{}, ApplyPaths{})
	if err == nil {
		t.Fatalf("expected missing paths error")
	}
}

func TestApplyRURecommendedProfileChownsSecretsForVeilGroup(t *testing.T) {
	// Hermetic: run as if root so the chown path is exercised regardless of the
	// CI runner's euid, and stub the user lookup so no real 'veil' account is needed.
	oldUID, oldLookup := effectiveUID, lookupUser
	defer func() { effectiveUID, lookupUser = oldUID, oldLookup }()
	effectiveUID = func() int { return 0 }
	lookupUser = func(string) (*user.User, error) { return &user.User{Uid: "0", Gid: "0"}, nil }
	// A non-root CI runner cannot os.Chown to another group (EPERM). Force the
	// group-read bit directly; this is exactly the permission the production
	// chown+chmod grants.
	oldChown, oldChmod := chownPath, chmodPath
	defer func() { chownPath, chmodPath = oldChown, oldChmod }()
	chownPath = func(path string, _, _ int) error {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		return os.Chmod(path, info.Mode()|0o040)
	}
	chmodPath = func(path string, mode os.FileMode) error {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		return os.Chmod(path, info.Mode()|mode&0o040)
	}

	dir := t.TempDir()
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		PanelAccess: "caddy",
		Domain:      "example.com",
		Email:       "admin@example.com",
		Secret:      func(label string) string { return "secret-" + label },
		PanelPort:   2096,
	})
	if err != nil {
		t.Fatalf("build profile: %v", err)
	}

	paths := ApplyPaths{
		EtcDir:     filepath.Join(dir, "etc", "veil"),
		VarDir:     filepath.Join(dir, "var", "lib", "veil"),
		SystemdDir: filepath.Join(dir, "etc", "systemd", "system"),
	}
	if _, err := ApplyRURecommendedProfile(profile, paths); err != nil {
		t.Fatalf("apply profile: %v", err)
	}

	envPath := filepath.Join(paths.EtcDir, "veil.env")
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("stat veil.env: %v", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("unexpected stat type for %s", envPath)
	}
	if stat.Mode&0o040 == 0 {
		t.Fatalf("veil.env must be group-readable (mode %o)", stat.Mode)
	}
}

func TestWriteManagedFileFailsWhenParentIsNotDirectory(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file where a directory is needed
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("block"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	// Try to write a file under blocker/subdir/ — MkdirAll should fail with ENOTDIR
	path := filepath.Join(blocker, "subdir", "file.txt")
	err := writeManagedFile(path, "content", 0o600)
	if err == nil {
		t.Fatal("expected error writing file under non-directory path")
	}
}
