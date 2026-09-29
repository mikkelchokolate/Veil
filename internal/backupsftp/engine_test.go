package backupsftp

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/testutil/sftpfake"
)

func testEngine(t *testing.T, fs *sftpfake.MemFS, dialErr error) Engine {
	t.Helper()
	dir := t.TempDir()
	engine := Engine{
		Paths: Paths{
			ConfigPath:     filepath.Join(dir, ConfigFileName),
			StatusPath:     filepath.Join(dir, StatusFileName),
			KnownHostsPath: filepath.Join(dir, KnownHostsFileName),
		},
		Dial: func(context.Context, Config, string) (RemoteFS, error) {
			if dialErr != nil {
				return nil, dialErr
			}
			return fs, nil
		},
		Now: func() time.Time { return time.Date(2026, 6, 5, 2, 0, 0, 0, time.UTC) },
	}
	return engine
}

func saveEngineConfig(t *testing.T, engine Engine, mutate func(*Config)) {
	t.Helper()
	config := sftpTestConfig()
	if mutate != nil {
		mutate(&config)
	}
	if err := engine.SaveConfig(config); err != nil {
		t.Fatal(err)
	}
}

func TestSyncArchiveUnconfiguredIsSilentNoop(t *testing.T) {
	called := false
	engine := testEngine(t, sftpfake.New(), nil)
	engine.Dial = func(context.Context, Config, string) (RemoteFS, error) {
		called = true
		return sftpfake.New(), nil
	}
	result, configured, err := engine.SyncArchive(context.Background(), writeLocalArchive(t, []byte("x")), "veil_backup_20260101_020000.tar.gz.enc", nil)
	if err != nil || configured || result.Uploaded != nil {
		t.Fatalf("unconfigured sync = %+v, %v, %v", result, configured, err)
	}
	if called {
		t.Fatal("dial attempted without a config")
	}
}

func TestSyncArchiveDisabledSkipsDial(t *testing.T) {
	fs := sftpfake.New()
	engine := testEngine(t, fs, nil)
	saveEngineConfig(t, engine, func(c *Config) { c.Enabled = false })
	engine.Dial = func(context.Context, Config, string) (RemoteFS, error) {
		t.Fatal("dial attempted for disabled destination")
		return nil, errors.New("unreachable")
	}
	_, configured, err := engine.SyncArchive(context.Background(), writeLocalArchive(t, []byte("x")), "veil_backup_20260101_020000.tar.gz.enc", nil)
	if err != nil || configured {
		t.Fatalf("disabled sync = %v, %v", configured, err)
	}
}

func TestSyncArchiveUploadsAndRecordsStatus(t *testing.T) {
	fs := sftpfake.New()
	engine := testEngine(t, fs, nil)
	saveEngineConfig(t, engine, nil)
	localPath := writeLocalArchive(t, []byte("archive-body"))
	name := filepath.Base(localPath)

	result, configured, err := engine.SyncArchive(context.Background(), localPath, name, nil)
	if err != nil || !configured || result.Uploaded == nil {
		t.Fatalf("sync = %+v, %v, %v", result, configured, err)
	}
	if !fs.Has("/srv/veil-backups/" + name) {
		t.Fatalf("remote missing archive: %v", fs.Paths())
	}
	status := engine.Status()
	if status.LastUploadAt != "2026-06-05T02:00:00Z" || status.LastUploadArchive != name || status.LastError != "" {
		t.Fatalf("status=%+v", status)
	}
	if !fs.Closed() {
		t.Fatal("remote connection was not closed")
	}
}

func TestSyncArchiveRecordsFailureWithoutLosingLocalResult(t *testing.T) {
	engine := testEngine(t, sftpfake.New(), errors.New("connection refused"))
	saveEngineConfig(t, engine, nil)

	_, configured, err := engine.SyncArchive(context.Background(), writeLocalArchive(t, []byte("x")), "veil_backup_20260101_020000.tar.gz.enc", nil)
	if err == nil || !configured {
		t.Fatalf("failed sync = %v, %v", err, configured)
	}
	status := engine.Status()
	if status.LastError == "" || status.LastErrorAt == "" {
		t.Fatalf("failure not recorded: %+v", status)
	}
}

func TestSyncArchiveMirrorsRetention(t *testing.T) {
	dir := "/srv/veil-backups"
	fs := sftpfake.New()
	engine := testEngine(t, fs, nil)
	saveEngineConfig(t, engine, nil)
	seedRemote(t, fs, dir, []string{
		"veil_backup_20260201_020000.tar.gz.enc",
		"veil_backup_20260202_020000.tar.gz.enc",
		"veil_backup_20260203_020000.tar.gz.enc",
	})

	localPath := writeLocalArchive(t, []byte("new"))
	policy := backup.RetentionPolicy{Daily: 1, Weekly: 0, Monthly: 0}
	result, configured, err := engine.SyncArchive(context.Background(), localPath, "veil_backup_20260204_020000.tar.gz.enc", &policy)
	if err != nil || !configured {
		t.Fatalf("sync+prune = %+v, %v, %v", result, configured, err)
	}
	if result.Uploaded == nil || len(result.Pruned) == 0 {
		t.Fatalf("sync+prune result=%+v", result)
	}
	status := engine.Status()
	if status.LastUploadAt == "" || status.LastPruneAt == "" || status.LastError != "" {
		t.Fatalf("status=%+v", status)
	}
}

func TestRemotePruneIfConfigured(t *testing.T) {
	fs := sftpfake.New()
	engine := testEngine(t, fs, nil)

	_, attempted, err := engine.RemotePruneIfConfigured(context.Background(), backup.DefaultRetentionPolicy())
	if err != nil || attempted {
		t.Fatalf("unconfigured prune = %v, %v", attempted, err)
	}

	saveEngineConfig(t, engine, func(c *Config) { c.Enabled = false })
	if _, attempted, err := engine.RemotePruneIfConfigured(context.Background(), backup.DefaultRetentionPolicy()); err != nil || attempted {
		t.Fatalf("disabled prune = %v, %v", attempted, err)
	}

	saveEngineConfig(t, engine, nil)
	seedRemote(t, fs, "/srv/veil-backups", []string{
		"veil_backup_20260201_020000.tar.gz.enc",
		"veil_backup_20260202_020000.tar.gz.enc",
	})
	result, attempted, err := engine.RemotePruneIfConfigured(context.Background(), backup.RetentionPolicy{Daily: 1})
	if err != nil || !attempted || len(result.Deleted) != 1 {
		t.Fatalf("prune = %+v, %v, %v", result, attempted, err)
	}
	if engine.Status().LastPruneAt == "" {
		t.Fatal("prune status not recorded")
	}
}

func TestRemoteListAndFetchRecordStatus(t *testing.T) {
	fs := sftpfake.New()
	engine := testEngine(t, fs, nil)
	saveEngineConfig(t, engine, nil)
	name := "veil_backup_20260101_020000.tar.gz.enc"
	fs.SetFile(path.Join("/srv/veil-backups", name), []byte("archive"))

	entries, err := engine.RemoteList(context.Background(), sftpTestConfig())
	if err != nil || len(entries) != 1 || entries[0].Name != name {
		t.Fatalf("list = %+v, %v", entries, err)
	}

	localDir := t.TempDir()
	entry, err := engine.FetchArchive(context.Background(), sftpTestConfig(), localDir, name)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != name {
		t.Fatalf("fetch entry=%+v", entry)
	}
	status := engine.Status()
	if status.LastFetchAt == "" || status.LastFetchArchive != name {
		t.Fatalf("fetch status=%+v", status)
	}

	if _, err := engine.RemoteList(context.Background(), sftpTestConfig()); err != nil {
		t.Fatal(err)
	}
}

func TestEngineRecordErrorSurfacesOnStatus(t *testing.T) {
	engine := testEngine(t, sftpfake.New(), errors.New("dial timeout"))
	saveEngineConfig(t, engine, nil)
	if _, err := engine.RemoteList(context.Background(), sftpTestConfig()); err == nil {
		t.Fatal("dial failure expected")
	}
	if status := engine.Status(); status.LastError == "" {
		t.Fatalf("error status=%+v", status)
	}
}

func TestLoadConfigEmptyPathIsNil(t *testing.T) {
	engine := Engine{}
	config, err := engine.LoadConfig()
	if err != nil || config != nil {
		t.Fatalf("empty path config = %v, %v", config, err)
	}
	// Status writes with no path must not panic or create anything.
	engine.recordStatus(func(s *Status) { s.LastError = "x" })
	if _, err := os.Stat(""); !os.IsNotExist(err) {
		t.Fatalf("stat empty path: %v", err)
	}
}
