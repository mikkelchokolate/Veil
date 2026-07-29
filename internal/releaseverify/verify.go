package releaseverify

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	sigbundle "github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

const githubActionsIssuer = "https://token.actions.githubusercontent.com"

// Evidence is the complete independently verifiable release evidence. The
// archive checksum is not trusted until both signed objects verify.
type Evidence struct {
	Repository       string
	WorkflowPath     string
	ReleaseTag       string
	ArchiveName      string
	Archive          []byte
	ChecksumsName    string
	Checksums        []byte
	ChecksumsBundle  []byte
	Provenance       []byte
	ProvenanceBundle []byte
}

var (
	trustedRootOnce sync.Once
	trustedRoot     root.TrustedMaterial
	trustedRootErr  error
)

func fetchTrustedRoot() (root.TrustedMaterial, error) {
	trustedRootOnce.Do(func() {
		trustedRoot, trustedRootErr = root.FetchTrustedRoot()
	})
	return trustedRoot, trustedRootErr
}

func Verify(e Evidence) error {
	if err := validateEvidencePresence(e); err != nil {
		return err
	}
	checksumsBundle, err := loadBundle(e.ChecksumsBundle)
	if err != nil {
		return fmt.Errorf("parse checksum bundle: %w", err)
	}
	provenanceBundle, err := loadBundle(e.ProvenanceBundle)
	if err != nil {
		return fmt.Errorf("parse provenance bundle: %w", err)
	}
	trusted, err := fetchTrustedRoot()
	if err != nil {
		return fmt.Errorf("load Sigstore trusted root: %w", err)
	}
	identity, err := expectedIdentity(e)
	if err != nil {
		return err
	}
	if err := verifyBlob(e.Checksums, checksumsBundle, trusted, identity); err != nil {
		return fmt.Errorf("verify checksum signature: %w", err)
	}
	if err := verifyBlob(e.Provenance, provenanceBundle, trusted, identity); err != nil {
		return fmt.Errorf("verify provenance signature: %w", err)
	}
	if err := verifyProvenanceSubjects(e); err != nil {
		return err
	}
	return nil
}

// VerifyBlob verifies one Sigstore bundle against an exact certificate identity
// and issuer. Callers must provide both values from trusted configuration.
func VerifyBlob(artifact, bundleJSON []byte, certificateIdentity, certificateIssuer string) error {
	if len(artifact) == 0 || len(bundleJSON) == 0 || strings.TrimSpace(certificateIdentity) == "" || strings.TrimSpace(certificateIssuer) == "" {
		return errors.New("signed blob evidence is incomplete")
	}
	bundle, err := loadBundle(bundleJSON)
	if err != nil {
		return err
	}
	trusted, err := fetchTrustedRoot()
	if err != nil {
		return err
	}
	identity, err := verify.NewShortCertificateIdentity(certificateIssuer, "", certificateIdentity, "")
	if err != nil {
		return err
	}
	return verifyBlob(artifact, bundle, trusted, identity)
}

func validateEvidencePresence(e Evidence) error {
	fields := []struct {
		name  string
		value any
	}{
		{"repository", e.Repository}, {"workflow path", e.WorkflowPath}, {"release tag", e.ReleaseTag},
		{"archive name", e.ArchiveName}, {"archive", e.Archive}, {"checksums", e.Checksums},
		{"checksum bundle", e.ChecksumsBundle}, {"provenance", e.Provenance}, {"provenance bundle", e.ProvenanceBundle},
	}
	for _, field := range fields {
		switch value := field.value.(type) {
		case string:
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("release evidence is missing %s", field.name)
			}
		case []byte:
			if len(value) == 0 {
				return fmt.Errorf("release evidence is missing %s", field.name)
			}
		}
	}
	if !strings.HasPrefix(e.ReleaseTag, "v") || strings.ContainsAny(e.ReleaseTag, "/\\") {
		return errors.New("release tag is not a canonical v* tag")
	}
	if e.WorkflowPath != ".github/workflows/release.yml" {
		return fmt.Errorf("unexpected release workflow %q", e.WorkflowPath)
	}
	return nil
}

func expectedIdentity(e Evidence) (verify.CertificateIdentity, error) {
	san := workflowIdentity(e.Repository, e.WorkflowPath, e.ReleaseTag)
	return verify.NewShortCertificateIdentity(githubActionsIssuer, "", san, "")
}

func workflowIdentity(repository, workflowPath, releaseTag string) string {
	return fmt.Sprintf("https://github.com/%s/%s@refs/tags/%s", repository, workflowPath, releaseTag)
}

func loadBundle(bundleJSON []byte) (*sigbundle.Bundle, error) {
	bundle := &sigbundle.Bundle{}
	if err := bundle.UnmarshalJSON(bundleJSON); err != nil {
		return nil, err
	}
	return bundle, nil
}

func verifyBlob(artifact []byte, bundle *sigbundle.Bundle, trusted root.TrustedMaterial, identity verify.CertificateIdentity) error {
	verifier, err := verify.NewVerifier(
		trusted,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
	if err != nil {
		return err
	}
	policy := verify.NewPolicy(verify.WithArtifact(bytes.NewReader(artifact)), verify.WithCertificateIdentity(identity))
	if _, err := verifier.Verify(bundle, policy); err != nil {
		return err
	}
	return nil
}

type provenanceStatement struct {
	Type          string              `json:"_type"`
	Subject       []provenanceSubject `json:"subject"`
	PredicateType string              `json:"predicateType"`
	Predicate     struct {
		BuildDefinition struct {
			BuildType            string         `json:"buildType"`
			ExternalParameters   map[string]any `json:"externalParameters"`
			ResolvedDependencies []struct {
				URI    string            `json:"uri"`
				Digest map[string]string `json:"digest"`
			} `json:"resolvedDependencies"`
		} `json:"buildDefinition"`
		RunDetails struct {
			Builder struct {
				ID string `json:"id"`
			} `json:"builder"`
		} `json:"runDetails"`
	} `json:"predicate"`
}

type provenanceSubject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

func verifyProvenanceSubjects(e Evidence) error {
	return verifyProvenanceStatement(e.Provenance, e)
}

func verifyProvenanceStatement(body []byte, e Evidence) error {
	var statement provenanceStatement
	if err := json.Unmarshal(body, &statement); err != nil {
		return fmt.Errorf("parse SLSA provenance: %w", err)
	}
	if statement.Type != "https://in-toto.io/Statement/v1" {
		return fmt.Errorf("unexpected provenance statement type %q", statement.Type)
	}
	if statement.PredicateType != "https://slsa.dev/provenance/v1" {
		return fmt.Errorf("unexpected provenance predicate %q", statement.PredicateType)
	}
	expectedWorkflow := workflowIdentity(e.Repository, e.WorkflowPath, e.ReleaseTag)
	if statement.Predicate.RunDetails.Builder.ID != expectedWorkflow {
		return fmt.Errorf("provenance builder %q does not match %q", statement.Predicate.RunDetails.Builder.ID, expectedWorkflow)
	}
	ref, _ := statement.Predicate.BuildDefinition.ExternalParameters["ref"].(string)
	if ref != "refs/tags/"+e.ReleaseTag {
		return fmt.Errorf("provenance ref %q does not match release tag", ref)
	}
	repository, _ := statement.Predicate.BuildDefinition.ExternalParameters["repository"].(string)
	if repository != "https://github.com/"+e.Repository {
		return fmt.Errorf("provenance repository %q does not match %q", repository, e.Repository)
	}
	if !hasSubject(statement.Subject, e.ArchiveName, digestHex(e.Archive)) {
		return fmt.Errorf("provenance has no subject digest for %s", e.ArchiveName)
	}
	checksumsName := e.ChecksumsName
	if checksumsName == "" {
		checksumsName = "checksums.txt"
	}
	if !hasSubject(statement.Subject, checksumsName, digestHex(e.Checksums)) {
		return fmt.Errorf("provenance has no subject digest for %s", checksumsName)
	}
	return nil
}

func hasSubject(subjects []provenanceSubject, name, digest string) bool {
	for _, subject := range subjects {
		if strings.TrimPrefix(subject.Name, "./") == name && strings.EqualFold(subject.Digest["sha256"], digest) {
			return true
		}
	}
	return false
}

func digestHex(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}
