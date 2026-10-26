package privileged

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPromotionQuarantinesAnUndecodableJournal locks #1267: the journal lives
// under the service-writable backup root, so a planted or corrupted file made
// every later promote fail on recovery — a persistent denial of service against
// the only path that publishes artifacts. Recovery must quarantine a journal it
// cannot interpret and let the operation proceed, keeping the evidence.
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
