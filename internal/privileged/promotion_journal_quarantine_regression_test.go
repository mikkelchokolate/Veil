package privileged

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPromotionQuarantinesAnUndecodableJournal locks #1267: the journal lives in
// the helper-owned backup root, so a crash, a legacy shape or a root-level
// mistake can leave a file recovery cannot interpret, and while it sat there
// every later promote failed — a persistent denial of service against the only
// path that publishes artifacts. Recovery must quarantine such a journal and let
// the retry proceed, keeping the evidence.
func TestPromotionQuarantinesAnUndecodableJournal(t *testing.T) {
	root := t.TempDir()
	request := preparePromotionFixture(t, root, 1)
	backups := filepath.Join(root, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backups, promotionTransactionJournalName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	withStubbedArtifactOwnership(t, func() {
		_, err := promoteResolvedArtifacts(backups, fixedPromotionNow, request)
		if err == nil || !strings.Contains(err.Error(), "quarantined") {
			t.Fatalf("first promote error = %v, want the one-shot quarantine failure", err)
		}
		if _, retryErr := promoteResolvedArtifacts(backups, fixedPromotionNow, request); retryErr != nil {
			t.Fatalf("promotion wedged by an undecodable journal: %v", retryErr)
		}
	})
	if _, err := os.Stat(filepath.Join(backups, promotionTransactionJournalName)); !os.IsNotExist(err) {
		t.Fatalf("journal still in place after recovery: %v", err)
	}
	entries, err := os.ReadDir(backups)
	if err != nil {
		t.Fatal(err)
	}
	quarantined := ""
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), promotionTransactionJournalName+".corrupt-") {
			quarantined = entry.Name()
		}
	}
	if quarantined == "" {
		t.Fatalf("no quarantined journal in %v", entries)
	}
	body, err := os.ReadFile(filepath.Join(backups, quarantined))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "{not json" {
		t.Fatalf("quarantined journal body = %q, want the evidence kept", body)
	}
}

// TestPromotionQuarantinesAJournalOutsideTheDestinationPolicy covers the other
// uninterpretable shape: a well-formed journal that names a destination the
// live policy rejects. It must not wedge promotions either.
func TestPromotionQuarantinesAJournalOutsideTheDestinationPolicy(t *testing.T) {
	root := t.TempDir()
	request := preparePromotionFixture(t, root, 1)
	backups := filepath.Join(root, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := `{"version":1,"transactionId":"planted","kind":"promote","phase":"prepared",` +
		`"manifestPath":"/tmp/planted/manifest.json","fenceGeneration":0,"manifest":{"records":[` +
		`{"id":"evil","destination":"/tmp/not-managed","phase":"prepared"}]}}`
	if err := os.WriteFile(filepath.Join(backups, promotionTransactionJournalName), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}

	withStubbedArtifactOwnership(t, func() {
		_, err := promoteResolvedArtifacts(backups, fixedPromotionNow, request)
		if err == nil || !strings.Contains(err.Error(), "quarantined") {
			t.Fatalf("first promote error = %v, want the one-shot quarantine failure", err)
		}
		if _, retryErr := promoteResolvedArtifacts(backups, fixedPromotionNow, request); retryErr != nil {
			t.Fatalf("promotion wedged by a foreign journal: %v", retryErr)
		}
	})
	entries, err := os.ReadDir(backups)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), promotionTransactionJournalName+".corrupt-") {
			found = true
		}
	}
	if !found {
		t.Fatalf("foreign journal was not quarantined: %v", entries)
	}
}

// TestPromotionRecoveryKeepsTheJournalWhenThePolicyIsMissing pins the line
// between "this journal is unusable" and "this caller is: a missing destination
// policy must refuse without moving the journal, or the correct caller could no
// longer recover (#1267 review).
func TestPromotionRecoveryKeepsTheJournalWhenThePolicyIsMissing(t *testing.T) {
	root := t.TempDir()
	backups := filepath.Join(root, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := `{"version":1,"transactionId":"planted","kind":"promote","phase":"prepared",` +
		`"manifestPath":"/tmp/planted/manifest.json","fenceGeneration":0,"manifest":{"records":[` +
		`{"id":"managed","destination":"/tmp/managed","phase":"prepared"}]}}`
	journalPath := filepath.Join(backups, promotionTransactionJournalName)
	if err := os.WriteFile(journalPath, []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverPromotionTransactionWithPolicy(backups, nil, 0); err == nil {
		t.Fatal("recovery accepted a journal without a destination policy")
	}
	if _, err := os.Stat(journalPath); err != nil {
		t.Fatalf("a valid journal was moved by a caller error: %v", err)
	}
}

// TestPromotionQuarantinesANonRegularJournal covers the other shape recovery
// cannot read: a directory left where the journal belongs used to fail every
// promote with "not a regular file".
func TestPromotionQuarantinesANonRegularJournal(t *testing.T) {
	root := t.TempDir()
	request := preparePromotionFixture(t, root, 1)
	backups := filepath.Join(root, "backups")
	if err := os.MkdirAll(filepath.Join(backups, promotionTransactionJournalName), 0o700); err != nil {
		t.Fatal(err)
	}
	withStubbedArtifactOwnership(t, func() {
		_, err := promoteResolvedArtifacts(backups, fixedPromotionNow, request)
		if err == nil || !strings.Contains(err.Error(), "quarantined") {
			t.Fatalf("first promote error = %v, want the one-shot quarantine failure", err)
		}
		if _, retryErr := promoteResolvedArtifacts(backups, fixedPromotionNow, request); retryErr != nil {
			t.Fatalf("promotion wedged by a non-regular journal: %v", retryErr)
		}
	})
}

// TestPromotionQuarantinesAFencingRegressedJournal keeps a journal from a
// "newer" generation from wedging the root: it is discarded, not replayed.
func TestPromotionQuarantinesAFencingRegressedJournal(t *testing.T) {
	root := t.TempDir()
	request := preparePromotionFixture(t, root, 1)
	backups := filepath.Join(root, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := `{"version":1,"transactionId":"future","kind":"promote","phase":"prepared",` +
		`"manifestPath":"/tmp/future/manifest.json","fenceGeneration":99,"manifest":{"records":[` +
		`{"id":"managed","destination":"/tmp/managed","phase":"prepared"}]}}`
	if err := os.WriteFile(filepath.Join(backups, promotionTransactionJournalName), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	withStubbedArtifactOwnership(t, func() {
		_, err := promoteResolvedArtifacts(backups, fixedPromotionNow, request)
		if err == nil || !strings.Contains(err.Error(), "quarantined") {
			t.Fatalf("first promote error = %v, want the one-shot quarantine failure", err)
		}
		if _, retryErr := promoteResolvedArtifacts(backups, fixedPromotionNow, request); retryErr != nil {
			t.Fatalf("promotion wedged by a fencing-regressed journal: %v", retryErr)
		}
	})
}
