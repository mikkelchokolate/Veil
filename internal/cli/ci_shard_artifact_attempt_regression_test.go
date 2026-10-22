package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #1367: after a partial rerun of one test shard, the aggregate job saw
// two artifacts for that shard (the failed attempt and the passing rerun) and
// merged the failed one. Shard artifacts are now attempt-scoped and the
// aggregate must resolve each shard to its newest attempt.
func TestCIShardArtifactsResolveNewestAttempt(t *testing.T) {
	checkBash(t)
	body, err := os.ReadFile("../../scripts/ci/test.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	start := strings.Index(script, "resolve_shard_artifact_dir() {")
	end := strings.Index(script, "merge_shards_stage() {")
	if start < 0 || end < start {
		t.Fatal("resolve_shard_artifact_dir must be defined before merge_shards_stage in test.sh")
	}
	helper := script[start:end]
	merge := script[end:]
	if !strings.Contains(merge, `d="$(resolve_shard_artifact_dir "${src}" "${i}")"`) {
		t.Fatal("merge_shards_stage must resolve shard dirs through resolve_shard_artifact_dir")
	}

	for _, tc := range []struct {
		name  string
		dirs  []string
		shard string
		want  string
		log   string
	}{
		{"partial rerun picks the passing attempt", []string{"ci-test-shard-2-attempt-1", "ci-test-shard-2-attempt-2"}, "2", "ci-test-shard-2-attempt-2", "shard 2: using attempt 2 (2 attempt artifact(s) found)"},
		{"attempts compare numerically", []string{"ci-test-shard-1-attempt-9", "ci-test-shard-1-attempt-10"}, "1", "ci-test-shard-1-attempt-10", "shard 1: using attempt 10 (2 attempt artifact(s) found)"},
		{"shard 1 ignores shard 10", []string{"ci-test-shard-1-attempt-1", "ci-test-shard-10-attempt-5"}, "1", "ci-test-shard-1-attempt-1", "shard 1: using attempt 1 (1 attempt artifact(s) found)"},
		{"non-numeric suffix is ignored", []string{"ci-test-shard-3-attempt-1", "ci-test-shard-3-attempt-x"}, "3", "ci-test-shard-3-attempt-1", "shard 3: using attempt 1 (1 attempt artifact(s) found)"},
		{"untouched shard keeps its only attempt", []string{"ci-test-shard-3-attempt-1"}, "3", "ci-test-shard-3-attempt-1", "shard 3: using attempt 1 (1 attempt artifact(s) found)"},
		{"legacy attempt-less layout still resolves", []string{"ci-test-shard-1"}, "1", "ci-test-shard-1", "shard 1: no attempt-scoped artifact, using legacy"},
		{"missing shard resolves to the legacy path (merge then fails closed)", nil, "2", "ci-test-shard-2", "shard 2: no attempt-scoped artifact, using legacy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, d := range tc.dirs {
				if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", "-c", "set -euo pipefail\n"+helper+"\nresolve_shard_artifact_dir \"$ROOT\" \"$SHARD\"")
			cmd.Env = append(os.Environ(), "ROOT="+root, "SHARD="+tc.shard)
			var stderr strings.Builder
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("resolve failed: %v: %s", err, stderr.String())
			}
			if got, want := strings.TrimSpace(string(out)), filepath.Join(root, tc.want); got != want {
				t.Fatalf("resolved %q, want %q", got, want)
			}
			// The chosen attempt must be logged so a silent fallback to an
			// older attempt is visible even when the merge succeeds.
			if !strings.Contains(stderr.String(), tc.log) {
				t.Fatalf("stderr %q does not report %q", stderr.String(), tc.log)
			}
		})
	}

	wf, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := stripHashComments(t, strings.ReplaceAll(string(wf), "\r\n", "\n"))
	if !strings.Contains(workflow, "name: ci-test-shard-${{ matrix.shard }}-attempt-${{ github.run_attempt }}") {
		t.Fatal("ci.yml must upload shard artifacts under an attempt-scoped name (#1367)")
	}
	if !strings.Contains(workflow, "pattern: ci-test-shard-*") {
		t.Fatal("ci.yml aggregate must download every shard attempt (pattern: ci-test-shard-*)")
	}
}
