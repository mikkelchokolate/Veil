package privileged

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

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

	result, err := runProductionBackup(context.Background(), ProductionConfig{BackupMaxBytes: 128 * 1024 * 1024}, ResolvedBackup{
		Action: BackupActionRead, ArchiveName: filepath.Base(path), ArchivePath: path,
		Offset: offset, Limit: int64(len(marker)),
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
