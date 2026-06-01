package installer

import (
	"os"
	"path/filepath"
	"testing"
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
	if len(result.WrittenFiles) != 4 {
		t.Fatalf("expected 4 written files, got %+v", result.WrittenFiles)
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

	assertFileContains(t, result.CaddyfilePath, "reverse_proxy 127.0.0.1:2096")
	assertFileContains(t, result.FallbackIndexPath, "Veil")
	assertFileMissing(t, result.Hysteria2Path)
	assertFileContains(t, filepath.Join(dir, "etc", "veil", "veil.env"), "VEIL_API_TOKEN=secret-panel")
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil.service"), "ExecStart=/usr/local/bin/veil serve")
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil-naive.service"), "caddy")
	assertFileMissing(t, filepath.Join(dir, "etc", "systemd", "system", "veil-hysteria2.service"))
	if len(result.WrittenFiles) != 5 {
		t.Fatalf("expected 5 written files, got %+v", result.WrittenFiles)
	}
}

func TestApplyRURecommendedProfileRejectsMissingPaths(t *testing.T) {
	_, err := ApplyRURecommendedProfile(RURecommendedProfile{}, ApplyPaths{})
	if err == nil {
		t.Fatalf("expected missing paths error")
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
