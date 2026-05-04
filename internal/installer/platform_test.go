package installer

import (
	"runtime"
	"testing"
)

func TestNormalizeArch(t *testing.T) {
	cases := map[string]string{
		"amd64":   "amd64",
		"x86_64":  "amd64",
		"arm64":   "arm64",
		"aarch64": "arm64",
	}
	for input, want := range cases {
		got, err := NormalizeArch(input)
		if err != nil {
			t.Fatalf("NormalizeArch(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeArch(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeArchRejectsUnsupported(t *testing.T) {
	if _, err := NormalizeArch("mips"); err == nil {
		t.Fatalf("expected unsupported arch error")
	}
}

func TestValidateLinuxPlatform(t *testing.T) {
	if err := ValidateLinuxPlatform(Platform{OS: "linux", Arch: "amd64"}); err != nil {
		t.Fatalf("expected supported platform: %v", err)
	}
	if err := ValidateLinuxPlatform(Platform{OS: "darwin", Arch: "amd64"}); err == nil {
		t.Fatalf("expected unsupported os error")
	}
}

func TestCurrentPlatform(t *testing.T) {
	p := CurrentPlatform()

	// OS must match runtime.GOOS
	if p.OS != runtime.GOOS {
		t.Fatalf("CurrentPlatform().OS = %q, want %q", p.OS, runtime.GOOS)
	}

	// Arch must match runtime.GOARCH
	if p.Arch != runtime.GOARCH {
		t.Fatalf("CurrentPlatform().Arch = %q, want %q", p.Arch, runtime.GOARCH)
	}

	// OS must be non-empty
	if p.OS == "" {
		t.Fatalf("CurrentPlatform().OS must be non-empty")
	}

	// Arch must be non-empty
	if p.Arch == "" {
		t.Fatalf("CurrentPlatform().Arch must be non-empty")
	}
}
