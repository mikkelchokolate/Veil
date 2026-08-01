package releaseverify

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestWorkflowIdentityBindsRepositoryWorkflowAndTag(t *testing.T) {
	got := workflowIdentity("mikkelchokolate/Veil", ".github/workflows/release.yml", "v1.2.3")
	want := "https://github.com/mikkelchokolate/Veil/.github/workflows/release.yml@refs/tags/v1.2.3"
	if got != want {
		t.Fatalf("identity=%q want=%q", got, want)
	}
}

func TestProvenanceStatementValidationMatrix(t *testing.T) {
	evidence := Evidence{
		Repository:    "mikkelchokolate/Veil",
		WorkflowPath:  ".github/workflows/release.yml",
		ReleaseTag:    "v1.2.3",
		ArchiveName:   "veil_linux_amd64.tar.gz",
		Archive:       []byte("archive"),
		ChecksumsName: "checksums.txt",
		Checksums:     []byte("checksums"),
	}
	valid := validProvenanceForTest(t, evidence)
	if err := verifyProvenanceStatement(valid, evidence); err != nil {
		t.Fatalf("valid statement: %v", err)
	}

	cases := map[string]func(map[string]any){
		"wrong repository": func(statement map[string]any) {
			predicate := statement["predicate"].(map[string]any)
			build := predicate["buildDefinition"].(map[string]any)
			build["externalParameters"].(map[string]any)["repository"] = "https://github.com/attacker/repo"
		},
		"wrong ref": func(statement map[string]any) {
			predicate := statement["predicate"].(map[string]any)
			build := predicate["buildDefinition"].(map[string]any)
			build["externalParameters"].(map[string]any)["ref"] = "refs/heads/main"
		},
		"wrong workflow": func(statement map[string]any) {
			predicate := statement["predicate"].(map[string]any)
			predicate["runDetails"].(map[string]any)["builder"].(map[string]any)["id"] = "https://github.com/attacker/repo/.github/workflows/release.yml@refs/tags/v1.2.3"
		},
		"wrong archive digest": func(statement map[string]any) {
			statement["subject"].([]any)[0].(map[string]any)["digest"].(map[string]any)["sha256"] = strings.Repeat("0", 64)
		},
		"missing checksums subject": func(statement map[string]any) {
			statement["subject"] = statement["subject"].([]any)[:1]
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var statement map[string]any
			if err := json.Unmarshal(valid, &statement); err != nil {
				t.Fatal(err)
			}
			mutate(statement)
			body, err := json.Marshal(statement)
			if err != nil {
				t.Fatal(err)
			}
			if err := verifyProvenanceStatement(body, evidence); err == nil {
				t.Fatal("expected provenance validation failure")
			}
		})
	}
}

func TestVerifyRejectsMalformedBundleBeforeTrustingChecksums(t *testing.T) {
	err := Verify(Evidence{
		Repository: "mikkelchokolate/Veil", WorkflowPath: ".github/workflows/release.yml", ReleaseTag: "v1.2.3",
		ArchiveName: "veil_linux_amd64.tar.gz", Archive: []byte("archive"),
		ChecksumsName: "checksums.txt", Checksums: []byte("checksums"),
		ChecksumsBundle: []byte(`{"not":"a sigstore bundle"}`),
		Provenance:      []byte(`{}`), ProvenanceBundle: []byte(`{"not":"a sigstore bundle"}`),
	})
	if err == nil {
		t.Fatal("expected malformed bundle rejection")
	}
}

func TestProvenanceRejectsWrongSourceCommitWithOtherwiseValidStatement(t *testing.T) {
	evidence := Evidence{
		Repository: "mikkelchokolate/Veil", WorkflowPath: ".github/workflows/release.yml", ReleaseTag: "v1.2.3",
		ArchiveName: "veil.tar.gz", Archive: []byte("archive"), ChecksumsName: "checksums.txt", Checksums: []byte("checksums"),
		SourceCommit: strings.Repeat("a", 40),
	}
	body := validProvenanceForTest(t, evidence)
	evidence.SourceCommit = strings.Repeat("b", 40)
	if err := verifyProvenanceStatement(body, evidence); err == nil || !strings.Contains(err.Error(), "source commit") {
		t.Fatalf("wrong signed source commit accepted: %v", err)
	}
}

func validProvenanceForTest(t *testing.T, evidence Evidence) []byte {
	t.Helper()
	digest := func(body []byte) string {
		sum := sha256.Sum256(body)
		return hex.EncodeToString(sum[:])
	}
	statement := map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"predicateType": "https://slsa.dev/provenance/v1",
		"subject": []any{
			map[string]any{"name": evidence.ArchiveName, "digest": map[string]any{"sha256": digest(evidence.Archive)}},
			map[string]any{"name": evidence.ChecksumsName, "digest": map[string]any{"sha256": digest(evidence.Checksums)}},
		},
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"buildType": "https://github.com/Attestations/GitHubActionsWorkflow@v1",
				"externalParameters": map[string]any{
					"repository": "https://github.com/" + evidence.Repository,
					"ref":        "refs/tags/" + evidence.ReleaseTag,
					"workflow":   evidence.WorkflowPath,
				},
			},
			"runDetails": map[string]any{
				"builder": map[string]any{"id": workflowIdentity(evidence.Repository, evidence.WorkflowPath, evidence.ReleaseTag)},
			},
		},
	}
	if evidence.SourceCommit != "" {
		build := statement["predicate"].(map[string]any)["buildDefinition"].(map[string]any)
		build["resolvedDependencies"] = []any{map[string]any{
			"uri":    "git+https://github.com/" + evidence.Repository,
			"digest": map[string]any{"gitCommit": evidence.SourceCommit},
		}}
	}
	body, err := json.Marshal(statement)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
