package update

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/releaseverify"
)

func TestRunWorkflowUpdatesProvenSourceBuildAndBindsSignedCommit(t *testing.T) {
	current := "main-aaaaaaa (" + strings.Repeat("a", 40) + ")"
	target := strings.Repeat("b", 40)
	deps, _ := newValidWorkflowDeps(t)
	deps.SourceUpdateCommit = func(gotCurrent, tag string) (string, error) {
		if gotCurrent != current || tag != "v1.2.4" {
			t.Fatalf("proof request = %q, %q", gotCurrent, tag)
		}
		return target, nil
	}
	verified := false
	deps.VerifyReleaseEvidence = func(evidence releaseverify.Evidence) error {
		if evidence.SourceCommit != target {
			t.Fatalf("signed evidence was not pinned to proven source: %q", evidence.SourceCommit)
		}
		verified = true
		return nil
	}
	installed := false
	deps.ReplaceBinaryFromArchive = func(currentPath string, _ []byte, _ bool) (string, error) {
		if !verified {
			t.Fatal("binary replacement preceded signed provenance verification")
		}
		installed = true
		return currentPath + ".backup", nil
	}
	var out bytes.Buffer
	if err := RunWorkflow(WorkflowOptions{CurrentVersion: current, Yes: true}, &out, deps); err != nil {
		t.Fatalf("proven source update: %v", err)
	}
	if !installed || !strings.Contains(out.String(), "release contains the running source commit") {
		t.Fatalf("update did not complete: installed=%v output=%s", installed, out.String())
	}
}

func TestRunWorkflowUnprovenSourceNeverDownloadsOrReplaces(t *testing.T) {
	for _, tc := range []struct {
		name, commit string
		err          error
	}{
		{"newer or diverged", "", errors.New("source is not an ancestor")},
		{"lookup failure", "", errors.New("GitHub unavailable")},
		{"missing commit", "", nil},
		{"ambiguous commit", "abcdef1", nil},
		{"malformed commit", strings.Repeat("z", 40), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps, _ := newValidWorkflowDeps(t)
			deps.SourceUpdateCommit = func(string, string) (string, error) { return tc.commit, tc.err }
			deps.DownloadAsset = func(string) ([]byte, error) { t.Fatal("unproven source downloaded an asset"); return nil, nil }
			deps.ReplaceBinaryFromArchive = func(string, []byte, bool) (string, error) {
				t.Fatal("unproven source replaced a binary")
				return "", nil
			}
			var out bytes.Buffer
			if err := RunWorkflow(WorkflowOptions{CurrentVersion: "main-" + strings.Repeat("a", 40)}, &out, deps); err == nil {
				t.Fatal("unproven transition did not fail")
			}
		})
	}
}

func TestRunWorkflowKeepsUnknownAndNonNewerReleaseRefusals(t *testing.T) {
	for _, current := range []string{"", "dev", "main-abcdef1", "v1.2.4", "v1.3.0"} {
		t.Run(current, func(t *testing.T) {
			deps, _ := newValidWorkflowDeps(t)
			deps.SourceUpdateCommit = func(string, string) (string, error) {
				t.Fatal("refused version invoked ancestry proof")
				return "", nil
			}
			deps.DownloadAsset = func(string) ([]byte, error) { t.Fatal("refused version downloaded assets"); return nil, nil }
			var out bytes.Buffer
			if err := RunWorkflow(WorkflowOptions{CurrentVersion: current}, &out, deps); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "--force") {
				t.Fatalf("missing existing force guidance: %s", out.String())
			}
		})
	}
}

func TestRunWorkflowForcedSourceBypassesAncestryButNotSignatures(t *testing.T) {
	deps, _ := newValidWorkflowDeps(t)
	deps.SourceUpdateCommit = func(string, string) (string, error) {
		t.Fatal("forced update invoked optional ancestry proof")
		return "", nil
	}
	verified := false
	deps.VerifyReleaseEvidence = func(releaseverify.Evidence) error { verified = true; return nil }
	var out bytes.Buffer
	if err := RunWorkflow(WorkflowOptions{CurrentVersion: "main-" + strings.Repeat("a", 40), Force: true, DryRun: true}, &out, deps); err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("forced update bypassed release signatures")
	}
}

func TestRunWorkflowSourceProofCannotBypassSignedTargetMismatch(t *testing.T) {
	deps, _ := newValidWorkflowDeps(t)
	target := strings.Repeat("b", 40)
	deps.SourceUpdateCommit = func(string, string) (string, error) { return target, nil }
	deps.VerifyReleaseEvidence = func(evidence releaseverify.Evidence) error {
		if evidence.SourceCommit != target {
			t.Fatalf("target not passed to signature verification: %q", evidence.SourceCommit)
		}
		return errors.New("signed provenance names another source commit")
	}
	deps.ReplaceBinaryFromArchive = func(string, []byte, bool) (string, error) {
		t.Fatal("mismatched signed target replaced binary")
		return "", nil
	}
	var out bytes.Buffer
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "main-" + strings.Repeat("a", 40), Yes: true}, &out, deps)
	if err == nil || !strings.Contains(err.Error(), "another source commit") {
		t.Fatalf("signature mismatch was not returned: %v", err)
	}
}
