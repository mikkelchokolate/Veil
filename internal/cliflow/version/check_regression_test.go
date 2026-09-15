package version

import "testing"

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
		{"v1.2.3 (8a5690c3f495609f224e51e55bce16af8d300603)", "v1.2.3", 0},
		{"v1.2.3-rc.1 (8a5690c3f495609f224e51e55bce16af8d300603)", "v1.2.3-rc.1", 0},
		{"v1.2.3 (8a5690c3f495609f224e51e55bce16af8d300603)", "v1.2.4", -1},
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
