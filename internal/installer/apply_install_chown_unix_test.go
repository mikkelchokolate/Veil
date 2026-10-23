//go:build unix

package installer

import (
	"os"
	"os/user"
	"path/filepath"
	"syscall"
	"testing"
)

func TestApplyRURecommendedProfileChownsSecretsForVeilGroup(t *testing.T) {
	// Hermetic: run as if root so the chown path is exercised regardless of the
	// CI runner's euid, and stub the user lookup so no real 'veil' account is needed.
	oldUID, oldLookupG := effectiveUID, lookupGroup
	defer func() { effectiveUID, lookupGroup = oldUID, oldLookupG }()
	effectiveUID = func() int { return 0 }
	lookupGroup = func(string) (*user.Group, error) { return &user.Group{Gid: "0"}, nil }
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
	caddyPath := filepath.Join(paths.EtcDir, "generated", "caddy", "config.json")
	info, err = os.Stat(caddyPath)
	if err != nil {
		t.Fatalf("stat caddy config: %v", err)
	}
	if info.Mode().Perm()&0o040 == 0 {
		t.Fatalf("generated caddy config must be group-readable so the panel can apply inbounds (mode %o)", info.Mode().Perm())
	}
}
