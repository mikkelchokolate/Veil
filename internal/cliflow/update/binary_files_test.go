package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateBinaryFilesReplacesAndRollsBackBinary(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	backupPath := currentPath + ".backup"
	files := NewBinaryFiles()

	if err := os.WriteFile(currentPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := files.Copy(currentPath, backupPath); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if err := files.ReplaceAtomic(currentPath, []byte("new")); err != nil {
		t.Fatalf("ReplaceAtomic: %v", err)
	}
	if err := files.Rollback(backupPath, currentPath); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	body, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old" {
		t.Fatalf("body = %q", string(body))
	}
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("backup should be removed after rollback")
	}
}

func TestBinaryFilesCopyReturnsOpenError(t *testing.T) {
	files := NewBinaryFiles()
	err := files.Copy(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "dst"))
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestBinaryFilesCopyReturnsCreateError(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := NewBinaryFiles()
	err := files.Copy(src, filepath.Join(dir, "nonexistent", "dst"))
	if err == nil {
		t.Fatal("expected error for unwritable destination")
	}
}

func TestBinaryFilesReplaceAtomicReturnsCreateError(t *testing.T) {
	files := NewBinaryFiles()
	err := files.ReplaceAtomic(filepath.Join(t.TempDir(), "missing", "dst"), []byte("x"))
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestBinaryFilesRollbackReturnsReadError(t *testing.T) {
	files := NewBinaryFiles()
	err := files.Rollback(filepath.Join(t.TempDir(), "missing.backup"), filepath.Join(t.TempDir(), "current"))
	if err == nil {
		t.Fatal("expected error for missing backup")
	}
}

func TestBinaryFilesRollbackCleansUpOnReplaceFailure(t *testing.T) {
	dir := t.TempDir()
	backupPath := filepath.Join(dir, "backup")
	if err := os.WriteFile(backupPath, []byte("backup"), 0o644); err != nil {
		t.Fatal(err)
	}
	files := NewBinaryFiles()
	err := files.Rollback(backupPath, filepath.Join(dir, "missing", "current"))
	if err == nil {
		t.Fatal("expected error restoring to missing directory")
	}
	if _, statErr := os.Stat(backupPath); os.IsNotExist(statErr) {
		t.Fatal("backup should remain when replace fails before temp creation")
	}
}

func TestReplaceBinaryFromArchiveReturnsExtractError(t *testing.T) {
	_, err := ReplaceBinaryFromArchive(filepath.Join(t.TempDir(), "veil"), []byte("not a tar.gz"), true)
	if err == nil {
		t.Fatal("expected extract error")
	}
}

func TestReplaceBinaryFromArchiveSkipsMissingBackup(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	archive := createTestTarGz(t, "veil", []byte("new-binary"))

	_, err := ReplaceBinaryFromArchive(currentPath, archive, false)
	if err == nil {
		t.Fatal("expected confirmation error")
	}
	if _, statErr := os.Stat(currentPath + ".backup"); !os.IsNotExist(statErr) {
		t.Fatal("backup should not be created when current binary is missing")
	}
}

func TestCopyFileDataAndReplaceBinaryAtomicAndRollbackBinary(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	backupPath := currentPath + ".backup"
	if err := os.WriteFile(currentPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CopyFileData(currentPath, backupPath); err != nil {
		t.Fatalf("CopyFileData: %v", err)
	}
	if err := ReplaceBinaryAtomic(currentPath, []byte("new")); err != nil {
		t.Fatalf("ReplaceBinaryAtomic: %v", err)
	}
	body, _ := os.ReadFile(currentPath)
	if string(body) != "new" {
		t.Fatalf("current = %q", string(body))
	}
	if err := RollbackBinary(backupPath, currentPath); err != nil {
		t.Fatalf("RollbackBinary: %v", err)
	}
	body, _ = os.ReadFile(currentPath)
	if string(body) != "old" {
		t.Fatalf("after rollback current = %q", string(body))
	}
}

type fakeAtomicFile struct {
	name       string
	writeErr   error
	closeErr   error
	closed     bool
	writeCalls int
}

func (f *fakeAtomicFile) Write(p []byte) (int, error) {
	f.writeCalls++
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *fakeAtomicFile) Close() error {
	f.closed = true
	return f.closeErr
}

func (f *fakeAtomicFile) Name() string { return f.name }

func TestReplaceAtomicReturnsWriteError(t *testing.T) {
	origCreate := createTempForReplace
	origRemove := removeForReplace
	createTempForReplace = func(string, string) (atomicFile, error) {
		return &fakeAtomicFile{name: filepath.Join(t.TempDir(), "tmp"), writeErr: errors.New("write failed")}, nil
	}
	removeForReplace = func(string) error { return nil }
	defer func() {
		createTempForReplace = origCreate
		removeForReplace = origRemove
	}()

	err := NewBinaryFiles().ReplaceAtomic(filepath.Join(t.TempDir(), "dst"), []byte("x"))
	if err == nil {
		t.Fatal("expected write error")
	}
}

func TestReplaceAtomicReturnsCloseError(t *testing.T) {
	origCreate := createTempForReplace
	origRemove := removeForReplace
	createTempForReplace = func(string, string) (atomicFile, error) {
		return &fakeAtomicFile{name: filepath.Join(t.TempDir(), "tmp"), closeErr: errors.New("close failed")}, nil
	}
	removeForReplace = func(string) error { return nil }
	defer func() {
		createTempForReplace = origCreate
		removeForReplace = origRemove
	}()

	err := NewBinaryFiles().ReplaceAtomic(filepath.Join(t.TempDir(), "dst"), []byte("x"))
	if err == nil {
		t.Fatal("expected close error")
	}
}

func TestReplaceAtomicReturnsChmodError(t *testing.T) {
	origCreate := createTempForReplace
	origChmod := chmodForReplace
	origRemove := removeForReplace
	createTempForReplace = func(string, string) (atomicFile, error) {
		return &fakeAtomicFile{name: filepath.Join(t.TempDir(), "tmp")}, nil
	}
	chmodForReplace = func(string, os.FileMode) error { return errors.New("chmod failed") }
	removeForReplace = func(string) error { return nil }
	defer func() {
		createTempForReplace = origCreate
		chmodForReplace = origChmod
		removeForReplace = origRemove
	}()

	err := NewBinaryFiles().ReplaceAtomic(filepath.Join(t.TempDir(), "dst"), []byte("x"))
	if err == nil {
		t.Fatal("expected chmod error")
	}
}

func TestReplaceAtomicReturnsRenameError(t *testing.T) {
	origCreate := createTempForReplace
	origChmod := chmodForReplace
	origRename := renameForReplace
	createTempForReplace = func(string, string) (atomicFile, error) {
		return &fakeAtomicFile{name: filepath.Join(t.TempDir(), "tmp")}, nil
	}
	chmodForReplace = func(string, os.FileMode) error { return nil }
	renameForReplace = func(string, string) error { return errors.New("rename failed") }
	defer func() {
		createTempForReplace = origCreate
		chmodForReplace = origChmod
		renameForReplace = origRename
	}()

	err := NewBinaryFiles().ReplaceAtomic(filepath.Join(t.TempDir(), "dst"), []byte("x"))
	if err == nil {
		t.Fatal("expected rename error")
	}
}
