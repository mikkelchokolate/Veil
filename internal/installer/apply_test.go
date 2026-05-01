package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil.service"), "ExecStart=/usr/local/bin/veil serve")
	if len(result.WrittenFiles) != 6 {
		t.Fatalf("expected 6 written files, got %+v", result.WrittenFiles)
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
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil.service"), "ExecStart=/usr/local/bin/veil serve")
	assertFileMissing(t, filepath.Join(dir, "etc", "systemd", "system", "veil-naive.service"))
	assertFileContains(t, filepath.Join(dir, "etc", "systemd", "system", "veil-hysteria2.service"), "hysteria2")
	if len(result.WrittenFiles) != 3 {
		t.Fatalf("expected 3 written files, got %+v", result.WrittenFiles)
	}
}

func TestApplyRURecommendedProfileRejectsMissingPaths(t *testing.T) {
	_, err := ApplyRURecommendedProfile(RURecommendedProfile{}, ApplyPaths{})
	if err == nil {
		t.Fatalf("expected missing paths error")
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("expected %s to be absent", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(body), want) {
		t.Fatalf("file %s missing %q:\n%s", path, want, string(body))
	}
}
