package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var _apply_install_apply_deps = []any{
	os.ReadFile, filepath.Join, strings.Contains, testing.T{},
}

func TestApplyRURecommendedProfileWritesGeneratedFiles(t *testing.T) {
	dir := t.TempDir()
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
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

	assertFileContains(t, result.CaddyfilePath, "forward_proxy")
	assertFileContains(t, result.Hysteria2Path, "listen: :443")
	assertFileContains(t, result.FallbackIndexPath, "Veil")
	assertFileContains(t, filepath.Join(dir, "etc", "veil", "veil.env"), "VEIL_API_TOKEN=secret-panel")
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil.service"), "ExecStart=/usr/local/bin/veil serve")
	if len(result.WrittenFiles) != 7 {
		t.Fatalf("expected 7 written files, got %+v", result.WrittenFiles)
	}
}

func TestApplyRURecommendedProfileWritesOnlySelectedStackFiles(t *testing.T) {
	dir := t.TempDir()
	profile, err := BuildRURecommendedProfile(RURecommendedInput{
		Domain:       "example.com",
		Email:        "admin@example.com",
		Stack:        StackHysteria2,
		Availability: PortAvailability{TCPBusy: map[int]bool{}, UDPBusy: map[int]bool{}},
		Secret:       func(label string) string { return "secret-" + label },
		RandomPort:   func() int { return 31874 },
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

	assertFileContains(t, result.Hysteria2Path, "listen: :443")
	assertFileMissing(t, result.CaddyfilePath)
	assertFileMissing(t, result.FallbackIndexPath)
	assertFileContains(t, filepath.Join(dir, "etc", "veil", "veil.env"), "VEIL_API_TOKEN=secret-panel")
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil.service"), "ExecStart=/usr/local/bin/veil serve")
	assertFileMissing(t, filepath.Join(dir, "etc", "systemd", "system", "veil-naive.service"))
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil-hysteria2.service"), "hysteria2")
	if len(result.WrittenFiles) != 4 {
		t.Fatalf("expected 4 written files, got %+v", result.WrittenFiles)
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
