package backupsftp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/testutil/sftpfake"
)

func sftpTestConfig() Config {
	return Config{
		Enabled: true, Host: "backups.example.com", User: "veil",
		RemoteDir: "/srv/veil-backups", AuthType: AuthTypePassword, Password: "pw",
	}
}

// writeLocalArchive writes a local file whose content passes the encrypted
// archive gate: the given body sits behind the Veil encryption magic so the
// fixture looks like a real .enc payload (#1188).
func writeLocalArchive(t *testing.T, body []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "veil_backup_20260101_020000.tar.gz.enc")
	payload := append([]byte("VEILBACK\x03"), body...)
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// localArchiveContent returns the bytes actually written by
// writeLocalArchive (encryption magic + body).
func localArchiveContent(t *testing.T, localPath string) []byte {
	t.Helper()
	body, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestUploadPublishesAtomicallyWithSidecar(t *testing.T) {
	localPath := writeLocalArchive(t, []byte("encrypted-backup-bytes"))
	want := localArchiveContent(t, localPath)
	name := filepath.Base(localPath)
	fs := sftpfake.New()

	receipt, err := Upload(context.Background(), fs, sftpTestConfig(), localPath, name)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(want)
	if receipt.Archive != name || receipt.Size != int64(len(want)) || receipt.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("receipt=%+v", receipt)
	}
	if got := fs.File("/srv/veil-backups/" + name); string(got) != string(want) {
		t.Fatalf("remote archive=%q", got)
	}
	if fs.Has("/srv/veil-backups/" + name + partialSuffix) {
		t.Fatal("stale .part left behind")
	}
	sidecar := string(fs.File("/srv/veil-backups/" + name + sidecarSuffix))
	wantSidecar := fmt.Sprintf("%s  %s", hex.EncodeToString(digest[:]), name)
	if !strings.Contains(sidecar, wantSidecar) {
		t.Fatalf("sidecar=%q, want substring %q", sidecar, wantSidecar)
	}
}

func TestUploadRejectsNonBasename(t *testing.T) {
	fs := sftpfake.New()
	for _, name := range []string{"", "../x.tar.gz.enc", "a/b.tar.gz.enc", `a\b`} {
		if _, err := Upload(context.Background(), fs, sftpTestConfig(), writeLocalArchive(t, []byte("x")), name); err == nil {
			t.Fatalf("upload accepted name %q", name)
		}
	}
}

// #1188: the remote tier is encrypted-only — a plaintext-named archive is
// refused before anything touches the remote.
func TestUploadRejectsUnencryptedArchiveName(t *testing.T) {
	fs := sftpfake.New()
	for _, name := range []string{
		"veil_backup_20260101_020000.tar.gz",
		"notes.txt",
	} {
		if _, err := Upload(context.Background(), fs, sftpTestConfig(), writeLocalArchive(t, []byte("x")), name); !errors.Is(err, ErrUnencryptedArchive) {
			t.Fatalf("upload name %q err=%v, want ErrUnencryptedArchive", name, err)
		}
	}
	if len(fs.Paths()) != 0 || len(fs.Written) != 0 {
		t.Fatalf("refused upload touched the remote: %v", fs.Paths())
	}
}

// #1188: a .enc-named file whose content was never encrypted (reachable via
// --output with an empty passphrase) is refused on the archive magic, again
// without writing anything remotely.
func TestUploadRejectsPlaintextContent(t *testing.T) {
	fs := sftpfake.New()
	dir := t.TempDir()
	localPath := filepath.Join(dir, "veil_backup_20260101_020000.tar.gz.enc")
	// A plaintext backup is a gzip stream — no VEILBACK magic.
	if err := os.WriteFile(localPath, []byte("\x1f\x8bplaintext-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Upload(context.Background(), fs, sftpTestConfig(), localPath, filepath.Base(localPath)); !errors.Is(err, ErrUnencryptedArchive) {
		t.Fatalf("plaintext upload err=%v, want ErrUnencryptedArchive", err)
	}
	if len(fs.Paths()) != 0 || len(fs.Written) != 0 {
		t.Fatalf("refused upload touched the remote: %v", fs.Paths())
	}
}

func TestUploadFailureCleansPartAndNeverPublishes(t *testing.T) {
	localPath := writeLocalArchive(t, []byte("payload"))
	name := filepath.Base(localPath)

	// Create failure: nothing lands remotely.
	fs := sftpfake.New()
	fs.Errors = map[string]error{"create": errors.New("disk full")}
	if _, err := Upload(context.Background(), fs, sftpTestConfig(), localPath, name); err == nil {
		t.Fatal("expected create failure")
	}
	if len(fs.Paths()) != 0 {
		t.Fatalf("remote has files after failed create: %v", fs.Paths())
	}

	// Rename failure: the .part must be cleaned up and the final absent.
	fs = sftpfake.New()
	fs.Errors = map[string]error{"rename": errors.New("rename rejected")}
	if _, err := Upload(context.Background(), fs, sftpTestConfig(), localPath, name); err == nil || !strings.Contains(err.Error(), "publish") {
		t.Fatalf("expected publish failure, got %v", err)
	}
	if fs.Has("/srv/veil-backups/"+name) || fs.Has("/srv/veil-backups/"+name+partialSuffix) {
		t.Fatalf("partial state left behind: %v", fs.Paths())
	}
}

// truncatingFS corrupts the destination on rename to exercise the
// post-publish size verification.
type truncatingFS struct{ *sftpfake.MemFS }

func (f truncatingFS) Rename(oldname, newname string) error {
	if err := f.MemFS.Rename(oldname, newname); err != nil {
		return err
	}
	f.MemFS.SetFile(path.Clean(newname), f.MemFS.File(path.Clean(newname))[:1])
	return nil
}

func TestUploadDetectsSizeMismatch(t *testing.T) {
	localPath := writeLocalArchive(t, []byte("payload-payload"))
	name := filepath.Base(localPath)
	fs := truncatingFS{sftpfake.New()}
	if _, err := Upload(context.Background(), fs, sftpTestConfig(), localPath, name); err == nil ||
		!strings.Contains(err.Error(), "size") {
		t.Fatalf("expected size verification failure, got %v", err)
	}
	// A verify-failed archive must not stay published (post-publish cleanup).
	if fs.Has("/srv/veil-backups/" + name) {
		t.Fatal("verify-failed archive left published on the remote")
	}
}

func TestUploadSidecarFailureRemovesPublishedArchive(t *testing.T) {
	localPath := writeLocalArchive(t, []byte("payload"))
	name := filepath.Base(localPath)
	fs := sftpfake.New()
	// Fail only the sidecar create — the archive itself publishes fine.
	fs.Errors = map[string]error{"create|" + path.Join("/srv/veil-backups", name+sidecarSuffix): errors.New("sidecar write failed")}
	if _, err := Upload(context.Background(), fs, sftpTestConfig(), localPath, name); err == nil {
		t.Fatal("expected sidecar write failure")
	}
	if fs.Has("/srv/veil-backups/"+name) || fs.Has("/srv/veil-backups/"+name+partialSuffix) {
		t.Fatalf("sidecar failure left published state: %v", fs.Paths())
	}
}

func seedRemote(t *testing.T, fs *sftpfake.MemFS, dir string, names []string) {
	t.Helper()
	if err := fs.MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		fs.SetFile(path.Join(dir, name), []byte("archive-"+name))
	}
}

func TestListFiltersManagedArchivesNewestFirst(t *testing.T) {
	fs := sftpfake.New()
	seedRemote(t, fs, "/srv/veil-backups", []string{
		"veil_backup_20260103_020000.tar.gz.enc",
		"veil_backup_20260101_020000.tar.gz.enc",
		"veil_backup_20260102_020000.tar.gz",
		"veil_backup_20260101_020000.tar.gz.enc.sha256",
		"notes.txt",
		"veil_backup_notadate.tar.gz.enc",
	})
	entries, err := List(context.Background(), fs, sftpTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries=%+v", entries)
	}
	if entries[0].Name != "veil_backup_20260103_020000.tar.gz.enc" ||
		entries[1].Name != "veil_backup_20260102_020000.tar.gz" ||
		entries[2].Name != "veil_backup_20260101_020000.tar.gz.enc" {
		t.Fatalf("ordering wrong: %+v", entries)
	}
	if !entries[0].Encrypted || entries[1].Encrypted {
		t.Fatalf("encrypted flags wrong: %+v", entries)
	}
}

func TestListMissingDirectoryIsEmpty(t *testing.T) {
	entries, err := List(context.Background(), sftpfake.New(), sftpTestConfig())
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing dir list = %v, %v", entries, err)
	}
}

func TestPruneMirrorsLocalRetentionDecision(t *testing.T) {
	dir := "/srv/veil-backups"
	fs := sftpfake.New()
	names := []string{
		"veil_backup_20260201_020000.tar.gz.enc",
		"veil_backup_20260202_020000.tar.gz.enc",
		"veil_backup_20260203_020000.tar.gz.enc",
		"veil_backup_20260204_020000.tar.gz.enc",
	}
	seedRemote(t, fs, dir, names)
	for _, name := range names {
		fs.SetFile(path.Join(dir, name+sidecarSuffix), []byte("deadbeef  "+name+"\n"))
	}

	entries, err := List(context.Background(), fs, sftpTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	policy := backup.RetentionPolicy{Daily: 2, Weekly: 0, Monthly: 0}
	wantKeep := backup.RetentionKeepSet(entries, policy)

	result, err := Prune(context.Background(), fs, sftpTestConfig(), policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if wantKeep[name] {
			if !fs.Has(path.Join(dir, name)) {
				t.Fatalf("kept archive %s was deleted", name)
			}
		} else {
			if fs.Has(path.Join(dir, name)) {
				t.Fatalf("archive %s survived prune", name)
			}
			if fs.Has(path.Join(dir, name+sidecarSuffix)) {
				t.Fatalf("sidecar for %s survived prune", name)
			}
		}
	}
	if len(result.Deleted)+len(result.Kept) != len(names) {
		t.Fatalf("prune result=%+v", result)
	}
	if err := fs.MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
}

func TestPruneNegativeCountsRejected(t *testing.T) {
	if _, err := Prune(context.Background(), sftpfake.New(), sftpTestConfig(), backup.RetentionPolicy{Daily: -1}); err == nil {
		t.Fatal("negative retention accepted")
	}
}

func TestFetchDownloadsAndVerifies(t *testing.T) {
	dir := "/srv/veil-backups"
	fs := sftpfake.New()
	name := "veil_backup_20260101_020000.tar.gz.enc"
	body := []byte("encrypted-archive")
	fs.SetFile(path.Join(dir, name), body)
	digest := sha256.Sum256(body)
	fs.SetFile(path.Join(dir, name+sidecarSuffix), []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), name)))

	localDir := t.TempDir()
	entry, err := Fetch(context.Background(), fs, sftpTestConfig(), localDir, name)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != name || entry.Size != int64(len(body)) || !entry.Encrypted {
		t.Fatalf("entry=%+v", entry)
	}
	got, err := os.ReadFile(filepath.Join(localDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("downloaded=%q", got)
	}
	// A second fetch must never replace the existing local archive.
	if _, err := Fetch(context.Background(), fs, sftpTestConfig(), localDir, name); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("refetch should refuse to overwrite, got %v", err)
	}
	// No temp files survive.
	locals, err := os.ReadDir(localDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(locals) != 1 || locals[0].Name() != name {
		t.Fatalf("local dir=%v", locals)
	}
}

func TestFetchWithoutSidecarStillPublishes(t *testing.T) {
	dir := "/srv/veil-backups"
	fs := sftpfake.New()
	name := "veil_backup_20260101_020000.tar.gz.enc"
	fs.SetFile(path.Join(dir, name), []byte("archive"))
	if _, err := Fetch(context.Background(), fs, sftpTestConfig(), t.TempDir(), name); err != nil {
		t.Fatal(err)
	}
}

func TestFetchRejectsCorruptSidecar(t *testing.T) {
	dir := "/srv/veil-backups"
	fs := sftpfake.New()
	name := "veil_backup_20260101_020000.tar.gz.enc"
	fs.SetFile(path.Join(dir, name), []byte("archive"))
	fs.SetFile(path.Join(dir, name+sidecarSuffix), []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  "+name+"\n"))
	localDir := t.TempDir()
	if _, err := Fetch(context.Background(), fs, sftpTestConfig(), localDir, name); err == nil ||
		!strings.Contains(err.Error(), "checksum") {
		t.Fatalf("expected checksum failure, got %v", err)
	}
	if entries, _ := os.ReadDir(localDir); len(entries) != 0 {
		t.Fatalf("failed fetch left files: %v", entries)
	}
}

func TestFetchRejectsBadNamesAndMissingRemote(t *testing.T) {
	fs := sftpfake.New()
	for _, name := range []string{"", "../state.json", "a/b.enc", "notes.txt", "veil_backup_bad.tar.gz.enc", "veil_backup_20260101_020000.tar.gz"} {
		if _, err := Fetch(context.Background(), fs, sftpTestConfig(), t.TempDir(), name); err == nil {
			t.Fatalf("fetch accepted %q", name)
		}
	}
	_, err := Fetch(context.Background(), fs, sftpTestConfig(), t.TempDir(), "veil_backup_20260101_020000.tar.gz.enc")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing remote should map to ErrNotExist, got %v", err)
	}
}
