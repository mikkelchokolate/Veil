package backupsftp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/testutil/sftpfake"
)

// testInstallID is the fixed per-test namespace identity so tests can seed
// the namespaced remote directory before an engine operation runs.
const testInstallID = "0123456789abcdef0123456789abcdef"

// testRemoteDir is this node's remote namespace under the shared remoteDir.
const testRemoteDir = "/srv/veil-backups/" + remoteNamespacePrefix + testInstallID

// testEngine builds an engine whose install id is already persisted, so the
// remote namespace is deterministic for pre-seeded fixtures.
func testEngine(t *testing.T, fs *sftpfake.MemFS, dialErr error) Engine {
	t.Helper()
	return testEngineWithID(t, fs, dialErr, testInstallID)
}

// testEngineWithID is testEngine with a caller-chosen install id — a second
// node sharing the remote filesystem in the multi-node tests.
func testEngineWithID(t *testing.T, fs *sftpfake.MemFS, dialErr error, installID string) Engine {
	t.Helper()
	dir := t.TempDir()
	installIDPath := filepath.Join(dir, InstallIDFileName)
	if err := os.WriteFile(installIDPath, []byte(installID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := Engine{
		Paths: Paths{
			ConfigPath:     filepath.Join(dir, ConfigFileName),
			StatusPath:     filepath.Join(dir, StatusFileName),
			KnownHostsPath: filepath.Join(dir, KnownHostsFileName),
			InstallIDPath:  installIDPath,
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
	if !fs.Has(testRemoteDir + "/" + name) {
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
	fs := sftpfake.New()
	engine := testEngine(t, fs, nil)
	saveEngineConfig(t, engine, nil)
	seedRemote(t, fs, testRemoteDir, []string{
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
	seedRemote(t, fs, testRemoteDir, []string{
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
	fs.SetFile(path.Join(testRemoteDir, name), []byte("archive"))

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

// writeNamedLocalArchive is writeLocalArchive with a caller-chosen basename,
// for fixtures where several archives must exist at once.
func writeNamedLocalArchive(t *testing.T, name, body string) string {
	t.Helper()
	localPath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(localPath, encryptedFixture([]byte(body)), 0o600); err != nil {
		t.Fatal(err)
	}
	return localPath
}

// #1188: a plaintext archive name is refused before the remote is even
// dialed — the operator's --allow-unencrypted consent covers only the local
// file.
func TestSyncArchiveRefusesUnencryptedArchiveBeforeDial(t *testing.T) {
	fs := sftpfake.New()
	engine := testEngine(t, fs, nil)
	saveEngineConfig(t, engine, nil)
	dialed := false
	engine.Dial = func(context.Context, Config, string) (RemoteFS, error) {
		dialed = true
		return fs, nil
	}
	localPath := filepath.Join(t.TempDir(), "veil_backup_20260101_020000.tar.gz")
	if err := os.WriteFile(localPath, []byte("\x1f\x8bplain"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, configured, err := engine.SyncArchive(context.Background(), localPath, filepath.Base(localPath), nil)
	if !errors.Is(err, ErrUnencryptedArchive) || !configured {
		t.Fatalf("plaintext sync = %v, %v", configured, err)
	}
	if dialed {
		t.Fatal("refused sync dialed the remote")
	}
	if len(fs.Paths()) != 0 {
		t.Fatalf("refused sync touched the remote: %v", fs.Paths())
	}
	if status := engine.Status(); status.LastError == "" {
		t.Fatal("refusal not recorded in status")
	}
}

// #1188: the .enc suffix alone is not enough — a .enc-named file whose
// content was never encrypted must also be refused before the dial.
func TestSyncArchiveRefusesEncNamedPlaintextBeforeDial(t *testing.T) {
	fs := sftpfake.New()
	engine := testEngine(t, fs, nil)
	saveEngineConfig(t, engine, nil)
	dialed := false
	engine.Dial = func(context.Context, Config, string) (RemoteFS, error) {
		dialed = true
		return fs, nil
	}
	localPath := filepath.Join(t.TempDir(), "veil_backup_20260101_020000.tar.gz.enc")
	if err := os.WriteFile(localPath, []byte("\x1f\x8bplain"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, configured, err := engine.SyncArchive(context.Background(), localPath, filepath.Base(localPath), nil)
	if !errors.Is(err, ErrUnencryptedArchive) || !configured {
		t.Fatalf(".enc-named plaintext sync = %v, %v", configured, err)
	}
	if dialed {
		t.Fatal("refused sync dialed the remote")
	}
	if len(fs.Paths()) != 0 {
		t.Fatalf("refused sync touched the remote: %v", fs.Paths())
	}
	if status := engine.Status(); status.LastError == "" {
		t.Fatal("refusal not recorded in status")
	}
}

// #1184: two installations sharing one remoteDir must never see, fetch, or
// prune each other's archives, and a pre-namespacing orphan at the remoteDir
// root must stay untouchable.
func TestRemoteNamespaceIsolatesSharedDirectory(t *testing.T) {
	ctx := context.Background()
	fs := sftpfake.New()
	otherID := "ffffffffffffffffffffffffffffffff"
	otherDir := "/srv/veil-backups/" + remoteNamespacePrefix + otherID

	engineA := testEngine(t, fs, nil)
	saveEngineConfig(t, engineA, nil)
	engineB := testEngineWithID(t, fs, nil, otherID)
	saveEngineConfig(t, engineB, nil)

	// An archive uploaded before namespacing existed sits unscoped at the
	// remoteDir root — the exact orphan the fix must never touch.
	orphan := "veil_backup_20250101_020000.tar.gz.enc"
	fs.SetFile(path.Join("/srv/veil-backups", orphan), []byte("legacy-orphan"))
	fs.SetFile(path.Join("/srv/veil-backups", orphan+sidecarSuffix), []byte("x"))

	// Each node uploads one archive per day, an hour apart — the exact repro
	// from the issue, where B's runs would eat A's history.
	days := []string{"20260201", "20260202", "20260203"}
	for _, day := range days {
		nameA := "veil_backup_" + day + "_020000.tar.gz.enc"
		if _, _, err := engineA.SyncArchive(ctx, writeNamedLocalArchive(t, nameA, "a-"+day), nameA, nil); err != nil {
			t.Fatalf("node A upload %s: %v", nameA, err)
		}
		nameB := "veil_backup_" + day + "_030000.tar.gz.enc"
		if _, _, err := engineB.SyncArchive(ctx, writeNamedLocalArchive(t, nameB, "b-"+day), nameB, nil); err != nil {
			t.Fatalf("node B upload %s: %v", nameB, err)
		}
	}

	// A's listing only contains A's namespace: neither B's archives nor the
	// root-level orphan are visible — fetch cannot pick them by accident.
	entries, err := engineA.RemoteList(ctx, sftpTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("node A remote list = %+v", entries)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name, "_020000.tar.gz.enc") {
			t.Fatalf("foreign archive listed for node A: %+v", entries)
		}
	}
	if _, err := engineA.FetchArchive(ctx, sftpTestConfig(), t.TempDir(), "veil_backup_20260203_030000.tar.gz.enc"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fetch of B's archive err=%v, want ErrNotExist", err)
	}
	if _, err := engineA.FetchArchive(ctx, sftpTestConfig(), t.TempDir(), orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fetch of the root orphan err=%v, want ErrNotExist", err)
	}

	// A prunes with daily=1: only A's newest survives. B's namespace and the
	// root orphan must be byte-for-byte intact.
	pruned, err := engineA.RemotePrune(ctx, sftpTestConfig(), backup.RetentionPolicy{Daily: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(pruned.Deleted) != 2 || len(pruned.Kept) != 1 {
		t.Fatalf("node A prune = %+v", pruned)
	}
	for _, day := range days {
		foreign := path.Join(otherDir, "veil_backup_"+day+"_030000.tar.gz.enc")
		if got := fs.File(foreign); !bytes.Equal(got, encryptedFixture([]byte("b-"+day))) {
			t.Fatalf("node B archive was touched: %s=%q", foreign, got)
		}
	}
	if got := fs.File(path.Join("/srv/veil-backups", orphan)); string(got) != "legacy-orphan" {
		t.Fatalf("root orphan was touched: %q", got)
	}
	if !fs.Has(path.Join("/srv/veil-backups", orphan+sidecarSuffix)) {
		t.Fatal("root orphan sidecar was removed")
	}
	if !fs.Has(path.Join(testRemoteDir, "veil_backup_20260203_020000.tar.gz.enc")) ||
		fs.Has(path.Join(testRemoteDir, "veil_backup_20260201_020000.tar.gz.enc")) {
		t.Fatalf("own namespace prune wrong: %v", fs.Paths())
	}
}

// The install id file is minted on first remote use and reused afterwards —
// every operation of the node lands in the same namespace.
func TestEngineMintsAndReusesInstallID(t *testing.T) {
	ctx := context.Background()
	fs := sftpfake.New()
	dir := t.TempDir()
	engine := Engine{
		Paths: Paths{
			ConfigPath:     filepath.Join(dir, ConfigFileName),
			StatusPath:     filepath.Join(dir, StatusFileName),
			KnownHostsPath: filepath.Join(dir, KnownHostsFileName),
		},
		Dial: func(context.Context, Config, string) (RemoteFS, error) { return fs, nil },
	}
	if err := engine.SaveConfig(sftpTestConfig()); err != nil {
		t.Fatal(err)
	}
	name := "veil_backup_20260101_020000.tar.gz.enc"
	if _, _, err := engine.SyncArchive(ctx, writeNamedLocalArchive(t, name, "x"), name, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, InstallIDFileName))
	if err != nil {
		t.Fatalf("install id not persisted: %v", err)
	}
	id := strings.TrimSpace(string(body))
	if !installIDPattern.MatchString(id) {
		t.Fatalf("persisted install id %q is malformed", id)
	}
	namespace := path.Join("/srv/veil-backups", namespaceDir(id))
	if !fs.Has(path.Join(namespace, name)) {
		t.Fatalf("archive not in node namespace: %v", fs.Paths())
	}
	name2 := "veil_backup_20260102_020000.tar.gz.enc"
	if _, _, err := engine.SyncArchive(ctx, writeNamedLocalArchive(t, name2, "y"), name2, nil); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if !fs.Has(path.Join(namespace, name2)) {
		t.Fatalf("second archive landed elsewhere: %v", fs.Paths())
	}
	if again, _ := os.ReadFile(filepath.Join(dir, InstallIDFileName)); strings.TrimSpace(string(again)) != id {
		t.Fatal("install id changed between runs")
	}
}

// A corrupt install id must fail closed: silently minting a second identity
// would strand the archives already uploaded under the first.
func TestRemoteOpFailsClosedOnMalformedInstallID(t *testing.T) {
	fs := sftpfake.New()
	engine := testEngine(t, fs, nil)
	saveEngineConfig(t, engine, nil)
	if err := os.WriteFile(engine.Paths.InstallIDPath, []byte("not-an-id"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RemoteList(context.Background(), sftpTestConfig()); err == nil ||
		!strings.Contains(err.Error(), "malformed") {
		t.Fatalf("malformed install id err=%v", err)
	}
	if len(fs.Paths()) != 0 {
		t.Fatalf("remote touched despite malformed id: %v", fs.Paths())
	}
}
