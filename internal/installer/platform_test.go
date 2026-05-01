package installer

import "testing"

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
