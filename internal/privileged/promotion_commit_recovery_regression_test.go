package privileged

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

func TestCommittedPromotionMarkerDeletionFailureFinalizesWithoutRollback(t *testing.T) {
	root := t.TempDir()
	request := preparePromotionFixture(t, root, 2)
	backupRoot := filepath.Join(root, "backups")
	originalRemove := promotionJournalRemove
	failed := false
	promotionJournalRemove = func(rootDir *safefs.Dir, name string) error {
		if !failed && name == promotionTransactionJournalName {
			failed = true
			return errors.New("injected committed marker unlink failure")
		}
		return originalRemove(rootDir, name)
	}
	defer func() { promotionJournalRemove = originalRemove }()

	withStubbedArtifactOwnership(t, func() {
		if _, err := promoteResolvedArtifacts(backupRoot, fixedPromotionNow, request); err == nil {
			t.Fatal("expected injected marker deletion error")
		}
	})
	assertPromotionSet(t, request, "new")

	promotionJournalRemove = originalRemove
	withStubbedArtifactOwnership(t, func() {
		recovery := ResolvedPromotion{ValidateDestination: allowPromotionDestinationsUnder(root)}
		if _, err := promoteResolvedArtifacts(backupRoot, fixedPromotionNow, recovery); err != nil {
			t.Fatalf("finalize committed promotion: %v", err)
		}
	})
	assertPromotionSet(t, request, "new")
	if _, err := os.Stat(filepath.Join(backupRoot, promotionTransactionJournalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("committed promotion marker remains: %v", err)
	}
}
