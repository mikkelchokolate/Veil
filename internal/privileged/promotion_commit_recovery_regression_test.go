package privileged

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCommittedPromotionMarkerDeletionFailureFinalizesWithoutRollback(t *testing.T) {
	root := t.TempDir()
	request := preparePromotionFixture(t, root, 2)
	backupRoot := filepath.Join(root, "backups")
	originalRemove := promotionJournalRemove
	failed := false
	promotionJournalRemove = func(path string) error {
		if !failed && filepath.Base(path) == promotionTransactionJournalName {
			failed = true
			return errors.New("injected committed marker unlink failure")
		}
		return os.Remove(path)
	}
	defer func() { promotionJournalRemove = originalRemove }()

	withNonRootPromotionHooks(t, func() {
		if _, err := promoteResolvedArtifacts(backupRoot, fixedPromotionNow, request); err == nil {
			t.Fatal("expected injected marker deletion error")
		}
	})
	assertPromotionSet(t, request, "new")

	promotionJournalRemove = originalRemove
	withNonRootPromotionHooks(t, func() {
		if _, err := promoteResolvedArtifacts(backupRoot, fixedPromotionNow, ResolvedPromotion{}); err != nil {
			t.Fatalf("finalize committed promotion: %v", err)
		}
	})
	assertPromotionSet(t, request, "new")
	if _, err := os.Stat(filepath.Join(backupRoot, promotionTransactionJournalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("committed promotion marker remains: %v", err)
	}
}
