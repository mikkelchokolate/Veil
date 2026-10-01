package privileged

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/backupsftp"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/testutil/sftpfake"
)

// sftpBackupFixture builds a fully formed state+key+passphrase fixture plus a
// production config with the SFTP dial seam pointed at the given fake.
type sftpBackupFixture struct {
	root       string
	statePath  string
	keyPath    string
	passPath   string
	backupRoot string
	config     ProductionConfig
	sftpPaths  BackupSftpPaths
	remote     *sftpfake.MemFS
	// remoteDir is this fixture's remote namespace under the configured
	// remoteDir — the only place the fake remote may see writes.
	remoteDir string
}

// sftpTestInstallID is the fixture's persisted identity, so the remote
// namespace is deterministic for pre-seeded archives (#1184).
const sftpTestInstallID = "0123456789abcdef0123456789abcdef"

func newSftpBackupFixture(t *testing.T, remote *sftpfake.MemFS, dialErr error) sftpBackupFixture {
	t.Helper()
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	passPath := filepath.Join(root, "backup.passphrase")
	backupRoot := filepath.Join(root, "backups")
	installIDPath := filepath.Join(root, backupsftp.InstallIDFileName)
	if err := os.WriteFile(installIDPath, []byte(sftpTestInstallID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, key[:], 0o600); err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := managementstate.NewStore(statePath, cipher).Save(model.ManagementSnapshot{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passPath, []byte("a-very-long-passphrase-32"), 0o600); err != nil {
		t.Fatal(err)
	}
	createBackupTestDatabase(t, root)
	fixture := sftpBackupFixture{
		root:       root,
		statePath:  statePath,
		keyPath:    keyPath,
		passPath:   passPath,
		backupRoot: backupRoot,
		remote:     remote,
		remoteDir:  "/srv/veil-backups/veil-node-" + sftpTestInstallID,
		sftpPaths: BackupSftpPaths{
			ConfigPath:     filepath.Join(root, "backup-sftp.json"),
			StatusPath:     filepath.Join(root, "backup-sftp-status.json"),
			KnownHostsPath: filepath.Join(root, "backup-sftp.known_hosts"),
			InstallIDPath:  installIDPath,
		},
	}
	fixture.config = ProductionConfig{
		StatePath:            statePath,
		KeyPath:              keyPath,
		BackupPassphrasePath: passPath,
		BackupRoot:           backupRoot,
		BackupMaxBytes:       8 * 1024 * 1024,
		VeilVersion:          "v0.0.1",
		Now:                  func() time.Time { return time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC) },
		SftpDial: func(context.Context, backupsftp.Config, string) (backupsftp.RemoteFS, error) {
			if dialErr != nil {
				return nil, dialErr
			}
			return remote, nil
		},
	}
	return fixture
}

func (f sftpBackupFixture) createRequest() ResolvedBackup {
	return ResolvedBackup{
		Action: BackupActionCreate, BackupRoot: f.backupRoot, StateRoot: f.root,
		StatePath: f.statePath, KeyPath: f.keyPath, BackupPassphrasePath: f.passPath,
		SftpPaths: f.sftpPaths,
	}
}

func (f sftpBackupFixture) saveConfig(t *testing.T, enabled bool) {
	t.Helper()
	err := backupsftp.SaveConfig(f.sftpPaths.ConfigPath, backupsftp.Config{
		Enabled: enabled, Host: "backups.example.com", User: "veil",
		RemoteDir: "/srv/veil-backups", AuthType: backupsftp.AuthTypePassword, Password: "pw",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBackupCreateUploadsToConfiguredSftp(t *testing.T) {
	remote := sftpfake.New()
	fixture := newSftpBackupFixture(t, remote, nil)
	fixture.saveConfig(t, true)

	result, err := runProductionBackup(context.Background(), fixture.config, fixture.createRequest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result.RemoteUpload == nil || result.RemoteUpload.Archive != result.ArchiveName {
		t.Fatalf("remote upload missing: %+v", result)
	}
	if result.RemoteError != "" || result.Warning != "" {
		t.Fatalf("unexpected remote error/warning: %+v", result)
	}
	if !remote.Has(fixture.remoteDir+"/"+result.ArchiveName) ||
		!remote.Has(fixture.remoteDir+"/"+result.ArchiveName+".sha256") {
		t.Fatalf("remote files=%v", remote.Paths())
	}
	status := backupsftp.LoadStatus(fixture.sftpPaths.StatusPath)
	if status.LastUploadAt == "" || status.LastUploadArchive != result.ArchiveName {
		t.Fatalf("status=%+v", status)
	}
}

func TestBackupCreateSftpFailureKeepsLocalSuccess(t *testing.T) {
	fixture := newSftpBackupFixture(t, sftpfake.New(), errors.New("connection refused"))
	fixture.saveConfig(t, true)

	result, err := runProductionBackup(context.Background(), fixture.config, fixture.createRequest())
	if err != nil {
		t.Fatalf("local create must not fail on remote error: %v", err)
	}
	if result.ArchiveName == "" || !result.Verified {
		t.Fatalf("local result lost: %+v", result)
	}
	if result.RemoteError == "" || !strings.Contains(result.Warning, "sftp upload failed") {
		t.Fatalf("remote failure not reported loudly: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(fixture.backupRoot, result.ArchiveName)); err != nil {
		t.Fatal(err)
	}
	if status := backupsftp.LoadStatus(fixture.sftpPaths.StatusPath); status.LastError == "" {
		t.Fatalf("status did not record failure: %+v", status)
	}
}

func TestBackupCreateWithoutSftpConfigSkipsRemote(t *testing.T) {
	remote := sftpfake.New()
	fixture := newSftpBackupFixture(t, remote, nil)
	// No config file: remote fields must stay empty and no dial happens.
	result, err := runProductionBackup(context.Background(), fixture.config, fixture.createRequest())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result.RemoteUpload != nil || result.RemoteError != "" {
		t.Fatalf("remote fields set without config: %+v", result)
	}
	if len(remote.Paths()) != 0 {
		t.Fatalf("remote touched without config: %v", remote.Paths())
	}
}

func TestBackupPruneMirrorsRemoteRetention(t *testing.T) {
	remote := sftpfake.New()
	fixture := newSftpBackupFixture(t, remote, nil)
	fixture.saveConfig(t, true)
	dir := fixture.remoteDir
	for _, name := range []string{
		"veil_backup_20260201_020000.tar.gz.enc",
		"veil_backup_20260202_020000.tar.gz.enc",
		"veil_backup_20260203_020000.tar.gz.enc",
	} {
		remote.SetFile(path.Join(dir, name), []byte("a"))
		remote.SetFile(path.Join(dir, name+".sha256"), []byte("x"))
	}
	// Foreign archives — another node's namespace plus a pre-namespacing
	// orphan at the remoteDir root — must never be prune candidates (#1184).
	foreignDir := "/srv/veil-backups/veil-node-ffffffffffffffffffffffffffffffff"
	foreignName := "veil_backup_20260202_020000.tar.gz.enc"
	remote.SetFile(path.Join(foreignDir, foreignName), []byte("other-node"))
	orphan := "veil_backup_20260101_020000.tar.gz.enc"
	remote.SetFile(path.Join("/srv/veil-backups", orphan), []byte("legacy-orphan"))

	result, err := runProductionBackup(context.Background(), fixture.config, ResolvedBackup{
		Action: BackupActionPrune, BackupRoot: fixture.backupRoot,
		Daily: 1, Weekly: 0, Monthly: 0, SftpPaths: fixture.sftpPaths,
	})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(result.RemotePruned) != 2 || len(result.RemoteKept) != 1 {
		t.Fatalf("remote prune=%+v", result)
	}
	if remote.Has(path.Join(dir, "veil_backup_20260202_020000.tar.gz.enc")) {
		t.Fatal("remote archive survived prune")
	}
	if !remote.Has(path.Join(foreignDir, foreignName)) {
		t.Fatal("foreign node archive was pruned")
	}
	if !remote.Has(path.Join("/srv/veil-backups", orphan)) {
		t.Fatal("root orphan archive was pruned")
	}
	if status := backupsftp.LoadStatus(fixture.sftpPaths.StatusPath); status.LastPruneAt == "" {
		t.Fatalf("prune status=%+v", status)
	}
}

func TestBackupSftpGetSetDeleteRoundtrip(t *testing.T) {
	fixture := newSftpBackupFixture(t, sftpfake.New(), nil)
	resolved := func(action BackupSftpAction, cfg *BackupSftpConfig, name string) ResolvedBackupSftp {
		return ResolvedBackupSftp{Action: action, Config: cfg, ArchiveName: name, BackupRoot: fixture.backupRoot, Paths: fixture.sftpPaths}
	}
	ctx := context.Background()

	// Not configured: get reports configured=false.
	got, err := runProductionBackupSftp(ctx, fixture.config, resolved(BackupSftpActionGet, nil, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got.Destination == nil || got.Destination.Configured {
		t.Fatalf("get=%+v", got.Destination)
	}
	if got.Status == nil {
		t.Fatal("status missing")
	}

	password := "initial-secret"
	set, err := runProductionBackupSftp(ctx, fixture.config, resolved(BackupSftpActionSet, &BackupSftpConfig{
		Enabled: true, Host: "backups.example.com", Port: 2222, User: "veil",
		RemoteDir: "/srv/veil-backups", AuthType: string(backupsftp.AuthTypePassword), Password: &password,
	}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !set.Destination.Configured || !set.Destination.Enabled || !set.Destination.PasswordSet ||
		set.Destination.Port != 2222 || set.Destination.Host != "backups.example.com" {
		t.Fatalf("set view=%+v", set.Destination)
	}
	info, err := os.Stat(fixture.sftpPaths.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 0600", info.Mode().Perm())
	}
	// The on-disk file must hold the secret; the result must not echo it.
	body, _ := os.ReadFile(fixture.sftpPaths.ConfigPath)
	if !strings.Contains(string(body), "initial-secret") {
		t.Fatal("secret not persisted")
	}
	encoded, _ := json.Marshal(set)
	if strings.Contains(string(encoded), "initial-secret") {
		t.Fatalf("set result leaked secret: %s", encoded)
	}

	// A follow-up set without a password keeps the stored secret.
	set2, err := runProductionBackupSftp(ctx, fixture.config, resolved(BackupSftpActionSet, &BackupSftpConfig{
		Enabled: true, Host: "backups.example.com", Port: 2222, User: "veil",
		RemoteDir: "/srv/other", AuthType: string(backupsftp.AuthTypePassword),
	}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !set2.Destination.PasswordSet || set2.Destination.RemoteDir != "/srv/other" {
		t.Fatalf("set2 view=%+v", set2.Destination)
	}
	persisted, err := backupsftp.LoadConfig(fixture.sftpPaths.ConfigPath)
	if err != nil || persisted.Password != "initial-secret" {
		t.Fatalf("secret lost on update: %v", err)
	}

	// Delete clears the destination.
	deleted, err := runProductionBackupSftp(ctx, fixture.config, resolved(BackupSftpActionDelete, nil, ""))
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Destination == nil || deleted.Destination.Configured {
		t.Fatalf("delete view=%+v", deleted.Destination)
	}
	if _, err := os.Stat(fixture.sftpPaths.ConfigPath); !os.IsNotExist(err) {
		t.Fatalf("config still present: %v", err)
	}
}

func TestBackupSftpListAndFetch(t *testing.T) {
	remote := sftpfake.New()
	fixture := newSftpBackupFixture(t, remote, nil)
	fixture.saveConfig(t, true)
	dir := fixture.remoteDir
	// No sidecar: the fetch relies on the encrypted-archive magic check for
	// pre-sidecar content (#1209).
	remote.SetFile(path.Join(dir, "veil_backup_20260101_020000.tar.gz.enc"), []byte("VEILBACK\x03remote-archive"))
	remote.SetFile(path.Join(dir, "notes.txt"), []byte("not-managed"))
	// An archive in another node's namespace is invisible to list/fetch (#1184).
	remote.SetFile("/srv/veil-backups/veil-node-ffffffffffffffffffffffffffffffff/veil_backup_20260102_020000.tar.gz.enc", []byte("other-node"))
	ctx := context.Background()

	listed, err := runProductionBackupSftp(ctx, fixture.config, ResolvedBackupSftp{
		Action: BackupSftpActionList, BackupRoot: fixture.backupRoot, Paths: fixture.sftpPaths,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Archives) != 1 || listed.Archives[0].Name != "veil_backup_20260101_020000.tar.gz.enc" {
		t.Fatalf("list=%+v", listed.Archives)
	}

	fetched, err := runProductionBackupSftp(ctx, fixture.config, ResolvedBackupSftp{
		Action: BackupSftpActionFetch, ArchiveName: "veil_backup_20260101_020000.tar.gz.enc",
		BackupRoot: fixture.backupRoot, Paths: fixture.sftpPaths,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Archive == nil || fetched.Archive.Name != "veil_backup_20260101_020000.tar.gz.enc" {
		t.Fatalf("fetch=%+v", fetched.Archive)
	}
	got, err := os.ReadFile(filepath.Join(fixture.backupRoot, "veil_backup_20260101_020000.tar.gz.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "VEILBACK\x03remote-archive" {
		t.Fatalf("fetched=%q", got)
	}
	if status := backupsftp.LoadStatus(fixture.sftpPaths.StatusPath); status.LastFetchArchive != "veil_backup_20260101_020000.tar.gz.enc" {
		t.Fatalf("fetch status=%+v", status)
	}
}

func TestBackupSftpListRequiresConfig(t *testing.T) {
	fixture := newSftpBackupFixture(t, sftpfake.New(), nil)
	_, err := runProductionBackupSftp(context.Background(), fixture.config, ResolvedBackupSftp{
		Action: BackupSftpActionList, BackupRoot: fixture.backupRoot, Paths: fixture.sftpPaths,
	})
	var perr *Error
	if !errors.As(err, &perr) || perr.Code != ErrorInvalidRequest {
		t.Fatalf("list without config err=%v", err)
	}
}

func TestResolveBackupSftpValidation(t *testing.T) {
	policy := Policy{
		StateRoot:                "/var/lib/veil",
		BackupRoot:               "/var/lib/veil/backups",
		BackupSftpConfigPath:     "/etc/veil/backup-sftp.json",
		BackupSftpStatusPath:     "/var/lib/veil/backup-sftp-status.json",
		BackupSftpKnownHostsPath: "/var/lib/veil/backup-sftp.known_hosts",
	}

	if resolved, err := policy.ResolveBackupSftp(BackupSftpRequest{Action: BackupSftpActionGet}); err != nil ||
		resolved.Paths.ConfigPath != "/etc/veil/backup-sftp.json" {
		t.Fatalf("get resolve=%+v, %v", resolved, err)
	}

	// Set requires a config body.
	if _, err := policy.ResolveBackupSftp(BackupSftpRequest{Action: BackupSftpActionSet}); err == nil {
		t.Fatal("set without config accepted")
	}
	// A set without a configured config path must fail closed.
	empty := Policy{BackupRoot: "/var/lib/veil/backups"}
	if _, err := empty.ResolveBackupSftp(BackupSftpRequest{
		Action: BackupSftpActionSet, Config: &BackupSftpConfig{Host: "h"},
	}); err == nil {
		t.Fatal("set resolved without config path")
	}

	// Fetch requires an .enc basename.
	for _, name := range []string{"", "a/b.enc", "..", "archive.txt", "../x.enc"} {
		if _, err := policy.ResolveBackupSftp(BackupSftpRequest{Action: BackupSftpActionFetch, ArchiveName: name}); err == nil {
			t.Fatalf("fetch accepted name %q", name)
		}
	}
	if _, err := policy.ResolveBackupSftp(BackupSftpRequest{
		Action: BackupSftpActionFetch, ArchiveName: "veil_backup_20260101_020000.tar.gz.enc",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := policy.ResolveBackupSftp(BackupSftpRequest{Action: "bogus"}); err == nil {
		t.Fatal("bogus action accepted")
	}
}

func TestLocalAdapterBackupSftpThroughPolicy(t *testing.T) {
	remote := sftpfake.New()
	fixture := newSftpBackupFixture(t, remote, nil)
	root := fixture.root
	policy := Policy{
		StateRoot:                root,
		BackupRoot:               fixture.backupRoot,
		BackupSftpConfigPath:     fixture.sftpPaths.ConfigPath,
		BackupSftpStatusPath:     fixture.sftpPaths.StatusPath,
		BackupSftpKnownHostsPath: fixture.sftpPaths.KnownHostsPath,
	}
	adapter := NewLocalAdapter(policy, NewProductionExecutor(fixture.config))
	ctx := context.Background()

	password := "pw"
	_, err := adapter.BackupSftp(ctx, BackupSftpRequest{
		Action: BackupSftpActionSet,
		Config: &BackupSftpConfig{
			Enabled: true, Host: "h", User: "u", RemoteDir: "/r",
			AuthType: string(backupsftp.AuthTypePassword), Password: &password,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapter.BackupSftp(ctx, BackupSftpRequest{Action: BackupSftpActionGet})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Destination.Configured || !got.Destination.PasswordSet {
		t.Fatalf("adapter get=%+v", got.Destination)
	}
}
