package privileged

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupReadTransactionKeepsExactOpenedInodeAcrossPathReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "archive.enc")
	oldBody := []byte("old-stable-content")
	if err := os.WriteFile(path, oldBody, 0o600); err != nil {
		t.Fatal(err)
	}
	config := ProductionConfig{BackupMaxBytes: 1024}
	first, err := runProductionBackup(context.Background(), config, ResolvedBackup{
		Action: BackupActionRead, ArchiveName: filepath.Base(path), ArchivePath: path, Limit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(root, "replacement")
	if err := os.WriteFile(replacement, []byte("new-hostile-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	second, err := runProductionBackup(context.Background(), config, ResolvedBackup{
		Action: BackupActionRead, ArchiveName: filepath.Base(path), ArchivePath: path,
		Offset: 3, Limit: int64(len(oldBody) - 3), TransactionID: first.TransactionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := append(append([]byte(nil), first.Data...), second.Data...); !bytes.Equal(got, oldBody) {
		t.Fatalf("stream reconstructed %q, want exact opened inode %q", got, oldBody)
	}
	if first.ContentDigest != second.ContentDigest || first.InodeGeneration != second.InodeGeneration || first.BoundSize != second.BoundSize {
		t.Fatalf("stream binding changed: first=%+v second=%+v", first, second)
	}
}

func TestProductionBackupReadUsesBoundedChunksBeyondLegacy64MiBLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.enc")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	const archiveBytes = int64(70 * 1024 * 1024)
	if err := file.Truncate(archiveBytes); err != nil {
		t.Fatal(err)
	}
	marker := []byte("beyond-the-old-helper-ceiling")
	offset := int64(65 * 1024 * 1024)
	if _, err := file.WriteAt(marker, offset); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	opened, err := runProductionBackup(context.Background(), ProductionConfig{BackupMaxBytes: 128 * 1024 * 1024}, ResolvedBackup{
		Action: BackupActionRead, ArchiveName: filepath.Base(path), ArchivePath: path, Limit: 1,
	})
	if err != nil {
		t.Fatalf("open stable read transaction: %v", err)
	}
	result, err := runProductionBackup(context.Background(), ProductionConfig{BackupMaxBytes: 128 * 1024 * 1024}, ResolvedBackup{
		Action: BackupActionRead, ArchiveName: filepath.Base(path), ArchivePath: path,
		Offset: offset, Limit: int64(len(marker)), TransactionID: opened.TransactionID,
	})
	if err != nil {
		t.Fatalf("runProductionBackup(read): %v", err)
	}
	if !bytes.Equal(result.Data, marker) {
		t.Fatalf("read chunk=%q want=%q", result.Data, marker)
	}
	if !result.More {
		t.Fatal("expected more data after bounded chunk")
	}
	_, _ = runProductionBackup(context.Background(), ProductionConfig{BackupMaxBytes: 128 * 1024 * 1024}, ResolvedBackup{
		Action: BackupActionRead, ArchiveName: filepath.Base(path), ArchivePath: path,
		Offset: archiveBytes, Limit: 1, TransactionID: opened.TransactionID,
	})
	if len(result.Data) > int(maxBackupReadChunkBytes) {
		t.Fatalf("helper returned oversized chunk: %d", len(result.Data))
	}

	link := filepath.Join(filepath.Dir(path), "linked-archive.enc")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := runProductionBackup(context.Background(), ProductionConfig{BackupMaxBytes: 128 * 1024 * 1024}, ResolvedBackup{
		Action: BackupActionRead, ArchiveName: filepath.Base(link), ArchivePath: link,
		Limit: 32,
	}); err == nil {
		t.Fatal("expected helper read to reject symlinked archive")
	}
}
