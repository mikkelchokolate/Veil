package privileged

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The staging root is writable by the veil uid, so the bytes the panel
// rendered and validated can be rewritten before the helper reads them. The
// promote request therefore carries the panel's digest of every staged
// artifact, and the helper refuses to publish bytes that do not match (#1269).

func stagedDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func writeStagedPromotionFixture(t *testing.T, policy Policy, staged, live []byte) (string, string) {
	t.Helper()
	source := filepath.Join(policy.StagingRoot, "caddy", "config.json")
	destination := filepath.Join(policy.GeneratedRoot, "caddy", "config.json")
	for _, dir := range []string{filepath.Dir(source), filepath.Dir(destination)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(source, staged, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, live, 0o600); err != nil {
		t.Fatal(err)
	}
	return source, destination
}

func TestPromotionRefusesStagedArtifactRewrittenAfterPanelDigest(t *testing.T) {
	stubRuntimeArtifactOwnership(t)
	policy := testPolicy(t)
	backupRoot := filepath.Join(policy.StateRoot, "promotion-backups")
	validated := []byte(`{"apps":{"validated":true}}`)
	live := []byte(`{"apps":{"live":true}}`)
	source, destination := writeStagedPromotionFixture(t, policy, validated, live)

	// The panel digests the bytes it rendered and validated ...
	request := PromoteRequest{
		ArtifactIDs:     []string{"caddy/config.json"},
		ArtifactDigests: map[string]string{"caddy/config.json": stagedDigest(validated)},
	}
	// ... and a veil-uid process rewrites the staged file afterwards.
	if err := os.WriteFile(source, []byte(`{"apps":{"attacker":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := policy.ResolvePromotion(request)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	executor := NewProductionExecutor(ProductionConfig{PromotionBackupRoot: backupRoot})
	_, err = executor.Promote(context.Background(), resolved)
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("promote of rewritten staged artifact err=%v, want digest mismatch", err)
	}
	body, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(body) != string(live) {
		t.Fatalf("destination=%s, want untouched live config", body)
	}
	if _, statErr := os.Stat(filepath.Join(backupRoot, promotionTransactionJournalName)); !os.IsNotExist(statErr) {
		t.Fatalf("rejected promotion left a transaction journal: %v", statErr)
	}
}

func TestPromotionPublishesStagedArtifactMatchingPanelDigest(t *testing.T) {
	stubRuntimeArtifactOwnership(t)
	policy := testPolicy(t)
	backupRoot := filepath.Join(policy.StateRoot, "promotion-backups")
	validated := []byte(`{"apps":{"validated":true}}`)
	_, destination := writeStagedPromotionFixture(t, policy, validated, []byte(`{"apps":{"live":true}}`))
	resolved, err := policy.ResolvePromotion(PromoteRequest{
		ArtifactIDs:     []string{"caddy/config.json"},
		ArtifactDigests: map[string]string{"caddy/config.json": stagedDigest(validated)},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	executor := NewProductionExecutor(ProductionConfig{PromotionBackupRoot: backupRoot})
	if _, err := executor.Promote(context.Background(), resolved); err != nil {
		t.Fatalf("promote matching staged artifact: %v", err)
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(validated) {
		t.Fatalf("destination=%s, want validated bytes", body)
	}
}

func TestPromotionTransactionRefusesStagedSourceWithoutExpectedDigest(t *testing.T) {
	stubRuntimeArtifactOwnership(t)
	policy := testPolicy(t)
	backupRoot := filepath.Join(policy.StateRoot, "promotion-backups")
	live := []byte(`{"apps":{"live":true}}`)
	source, destination := writeStagedPromotionFixture(t, policy, []byte(`{"apps":{"staged":true}}`), live)
	_, err := executePromotionTransactionFenced(backupRoot, nil, "promotion",
		[]ResolvedArtifact{{ID: "caddy/config.json", Source: source, Destination: destination}}, nil,
		0, policy.promotionDestinationAllowed)
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("promotion without expected digest err=%v, want refusal", err)
	}
	body, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(body) != string(live) {
		t.Fatalf("destination=%s, want untouched live config", body)
	}
}

func TestResolvePromotionRequiresWellFormedDigestPerStagedArtifact(t *testing.T) {
	policy := testPolicy(t)
	valid := stagedDigest([]byte("config"))
	cases := map[string]PromoteRequest{
		"missing digest": {ArtifactIDs: []string{"mieru"}},
		"empty digest":   {ArtifactIDs: []string{"mieru"}, ArtifactDigests: map[string]string{"mieru": ""}},
		"short digest":   {ArtifactIDs: []string{"mieru"}, ArtifactDigests: map[string]string{"mieru": valid[:63]}},
		"uppercase digest": {ArtifactIDs: []string{"mieru"},
			ArtifactDigests: map[string]string{"mieru": strings.ToUpper(valid)}},
		"non-hex digest": {ArtifactIDs: []string{"mieru"},
			ArtifactDigests: map[string]string{"mieru": strings.Repeat("z", 64)}},
		"digest for unrequested artifact": {ArtifactIDs: []string{"mieru"},
			ArtifactDigests: map[string]string{"mieru": valid, "caddy/config.json": valid}},
		"digest for removal": {RemoveArtifactIDs: []string{"mieru"},
			ArtifactDigests: map[string]string{"mieru": valid}},
		"digest on restore": {RestoreBackupID: "20260605T120000.000000000Z",
			ArtifactDigests: map[string]string{"mieru": valid}},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := policy.ResolvePromotion(request); err == nil {
				t.Fatalf("ResolvePromotion(%+v) accepted", request)
			} else {
				assertOperationErrorCode(t, err, ErrorInvalidRequest)
			}
		})
	}
	resolved, err := policy.ResolvePromotion(PromoteRequest{ArtifactIDs: []string{"mieru"},
		ArtifactDigests: map[string]string{"mieru": valid}})
	if err != nil {
		t.Fatalf("well-formed request: %v", err)
	}
	if len(resolved.Artifacts) != 1 || resolved.Artifacts[0].expectedDigest != valid {
		t.Fatalf("expected digest not carried to the resolved artifact: %+v", resolved.Artifacts)
	}
}

// attestStagedArtifacts gives every staged write the digest an honest panel
// would attest: the hash of the bytes currently at its Source. Tests that
// exercise other promotion properties use it so they keep failing for the
// reason they were written for, not for a missing digest.
func attestStagedArtifacts(t testing.TB, artifacts []ResolvedArtifact) []ResolvedArtifact {
	t.Helper()
	attested := append([]ResolvedArtifact(nil), artifacts...)
	for index := range attested {
		body, err := os.ReadFile(attested[index].Source)
		if err != nil {
			t.Fatalf("attest staged artifact %s: %v", attested[index].ID, err)
		}
		attested[index].expectedDigest = promotionDigest(body)
	}
	return attested
}

// attestedPromoteRequest attaches the digest of every staged file the policy
// resolves for ids, as the panel does from the bytes it rendered.
func attestedPromoteRequest(t testing.TB, policy Policy, ids ...string) PromoteRequest {
	t.Helper()
	artifacts, err := policy.resolveArtifacts(ids, false)
	if err != nil {
		t.Fatalf("resolve artifacts %v: %v", ids, err)
	}
	digests := make(map[string]string, len(artifacts))
	for _, artifact := range attestStagedArtifacts(t, artifacts) {
		digests[artifact.ID] = artifact.expectedDigest
	}
	return PromoteRequest{ArtifactIDs: ids, ArtifactDigests: digests}
}

// placeholderPromoteRequest carries well-formed digests for tests whose
// executor is stubbed or that only exercise request resolution.
func placeholderPromoteRequest(ids ...string) PromoteRequest {
	digests := make(map[string]string, len(ids))
	for _, id := range ids {
		digests[id] = promotionDigest([]byte("placeholder:" + id))
	}
	return PromoteRequest{ArtifactIDs: ids, ArtifactDigests: digests}
}

func ptrPromoteRequest(request PromoteRequest) *PromoteRequest { return &request }
