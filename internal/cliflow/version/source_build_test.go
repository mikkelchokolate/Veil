package version

import (
	"strings"
	"testing"
)

func TestSourceBuildCommitRecognizesOnlyUnambiguousMatchingMainBuilds(t *testing.T) {
	sha := "ae5124ee9d0c651f86c9c3bc99c021056fc0b699"
	for _, stamp := range []string{"main-" + sha, "main-ae5124ee9d0c (" + sha + ")", " main-ae5124e (" + sha + ") ", "main-AE5124E (" + strings.ToUpper(sha) + ")"} {
		if got := SourceBuildCommit(stamp); got != sha {
			t.Errorf("SourceBuildCommit(%q) = %q, want %s", stamp, got, sha)
		}
	}
	for _, stamp := range []string{"", "dev", "main", "main-ae5124ee9d0c", "dev (" + sha + ")", "v0.7.3 (" + sha + ")", "main-ae5124 (" + sha + ")", "main-deadbee (" + sha + ")", "main-ae5124e (" + sha[:39] + ")", "main-ae5124e (" + sha + ") extra", "main-ae5124e (" + sha + "))", "main-ae5124e (" + sha + "z)", "main-" + sha + "0"} {
		if got := SourceBuildCommit(stamp); got != "" {
			t.Errorf("SourceBuildCommit(%q) = %q, want no proof identity", stamp, got)
		}
	}
}
