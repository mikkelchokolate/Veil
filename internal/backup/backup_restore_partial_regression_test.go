package backup

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreFailureLeavesOriginalGenerationIntact(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	aPath := filepath.Join(root, "a.conf")
	bPath := filepath.Join(root, "b.conf")
	if err := os.WriteFile(aPath, []byte("old-A"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, []byte("old-B"), 0o600); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewLifecycle(backupDir)
	id, err := lifecycle.BackupExisting([]string{aPath, bPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aPath, []byte("current-A"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, []byte("current-B"), 0o600); err != nil {
		t.Fatal(err)
	}

	copies := 0
	origCopy := fileCopierCopy
	t.Cleanup(func() { fileCopierCopy = origCopy })
	fileCopierCopy = func(dst io.Writer, src io.Reader) (int64, error) {
		copies++
		// Safety backup copies both live files first; fail on the second restore member.
		if copies == 4 {
			return 0, errors.New("injected restore copy failure")
		}
		return io.Copy(dst, src)
	}

	_, err = lifecycle.Restore(id)
	if err == nil {
		t.Fatal("expected restore failure")
	}
	if !strings.Contains(err.Error(), "safety backup") {
		t.Fatalf("error should mention safety backup ID: %v", err)
	}

	gotA, err := os.ReadFile(aPath)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := os.ReadFile(bPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotA) != "current-A" || string(gotB) != "current-B" {
		t.Fatalf("mixed state a=%q b=%q; want current-A / current-B", gotA, gotB)
	}
}

func TestRestoreCommitFailureRestoresSafetyGeneration(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "backups")
	aPath := filepath.Join(root, "a.conf")
	bPath := filepath.Join(root, "b.conf")
	if err := os.WriteFile(aPath, []byte("old-A"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, []byte("old-B"), 0o600); err != nil {
		t.Fatal(err)
	}

	lifecycle := NewLifecycle(backupDir)
	id, err := lifecycle.BackupExisting([]string{aPath, bPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aPath, []byte("current-A"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bPath, []byte("current-B"), 0o600); err != nil {
		t.Fatal(err)
	}

	commits := 0
	origRename := restoreCommitRename
	t.Cleanup(func() { restoreCommitRename = origRename })
	restoreCommitRename = func(oldpath, newpath string) error {
		commits++
		if commits == 2 {
			return errors.New("injected restore commit failure")
		}
		return os.Rename(oldpath, newpath)
	}

	_, err = lifecycle.Restore(id)
	if err == nil {
		t.Fatal("expected restore failure")
	}

	gotA, err := os.ReadFile(aPath)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := os.ReadFile(bPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotA) != "current-A" || string(gotB) != "current-B" {
		t.Fatalf("mixed state a=%q b=%q; want current-A / current-B", gotA, gotB)
	}
}
