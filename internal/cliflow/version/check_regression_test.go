package version

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompareTreatsPrereleaseAsOlderThanMatchingStable(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.7.0-rc1", "v0.7.0", -1},
		{"v0.7.0-rc.1", "v0.7.0", -1},
		{"1.2.0-rc.1", "1.2.0", -1},
		{"v1.2.0-rc.1", "v1.2.0", -1},
		{"v1.2.0", "v1.2.0-rc.1", 1},
		{"v1.2.0-rc.2", "v1.2.0-rc.10", -1},
		{"v1.2.0-rc.10", "v1.2.0-rc.2", 1},
		{"v0.7.0+build.9", "v0.7.0", 0},
		{"v0.7.0+build.9", "v0.7.0+other", 0},
		{"v1.2.0", "1.2.0", 0},
		{"v1.2.3 (" + strings.Repeat("a", 40) + ")", "v1.2.3", 0},
		{"v1.2.3 (" + strings.Repeat("a", 40) + ")", "v1.2.4", -1},
		{"v1.2.4 (" + strings.Repeat("a", 40) + ")", "v1.2.3", 1},
		{"v1.2.3-rc.1 (" + strings.Repeat("a", 40) + ")", "v1.2.3", -1},
		{"v1.2.3-rc.1 (8a5690c3f495609f224e51e55bce16af8d300603)", "v1.2.3-rc.1", 0},
		{"dev", "v1.2.0", -1},
		{"(devel)", "v1.0.0", -1},
		{"1.abc", "1.0.0", -1},
		{"v1.0.0", "not-a-version", 1},
		{"bogus", "also-bogus", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestReleaseTagStripsCommitDisplaySuffix(t *testing.T) {
	sha := strings.Repeat("a", 40)
	if got := ReleaseTag("v1.2.3 (" + sha + ")"); got != "v1.2.3" {
		t.Fatalf("ReleaseTag(display) = %q", got)
	}
	if got := ReleaseTag("v1.2.3"); got != "v1.2.3" {
		t.Fatalf("ReleaseTag(tag) = %q", got)
	}
	if got := ReleaseTag("v1.2.3 (not-a-commit)"); got != "v1.2.3 (not-a-commit)" {
		t.Fatalf("ReleaseTag(non-sha) = %q", got)
	}
}

// TestIsReleaseVersion is the #1104 discriminator: only a parseable semver
// release (optionally with prerelease/build metadata or the release display
// suffix) counts as a release build. install-main "main-<sha>" stamps, dev
// builds, and empty strings are non-release — callers must not treat them as
// merely "older than" a tag or they silently downgrade a source build.
func TestIsReleaseVersion(t *testing.T) {
	for _, v := range []string{"v0.7.2", "0.7.2", "v0.8.0-rc1", "v1.2.3+build.9", "v1.2.3 (" + strings.Repeat("a", 40) + ")"} {
		if !IsReleaseVersion(v) {
			t.Errorf("IsReleaseVersion(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"main-abcdef1", "dev", "(devel)", "", "bogus"} {
		if IsReleaseVersion(v) {
			t.Errorf("IsReleaseVersion(%q) = true, want false", v)
		}
	}
}

// TestCheckReportsNonReleaseBuild is the #1104 regression on the version-check
// side: a main-<sha> build must be reported as a non-release build against the
// latest tag — not as "newer release available", which reads as advice to
// downgrade.
func TestCheckReportsNonReleaseBuild(t *testing.T) {
	var out bytes.Buffer
	check := NewCheck("main-abcdef1", &out, func() (string, error) { return "v0.7.2", nil })
	if err := check.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "Newer release available") {
		t.Fatalf("non-release build read as downgrade advice:\n%s", got)
	}
	if !strings.Contains(got, "non-release build") {
		t.Fatalf("expected non-release build notice, got:\n%s", got)
	}
}

func TestCheckReportsUpToDateWithReleaseCommitDisplay(t *testing.T) {
	var out bytes.Buffer
	display := "v1.2.3 (" + strings.Repeat("a", 40) + ")"
	check := NewCheck(display, &out, func() (string, error) { return "v1.2.3", nil })
	if err := check.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "Newer release available") {
		t.Fatalf("release display version looked outdated:\n%s", got)
	}
	if !strings.Contains(got, "Veil is up to date ("+display+").") {
		t.Fatalf("output = %s", got)
	}
}
