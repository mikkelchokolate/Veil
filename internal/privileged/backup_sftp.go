package privileged

import (
	"context"
	"path/filepath"
	"time"

	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/backupsftp"
)

// SFTP remote-backup destination wiring for the privileged helper.
//
// The helper owns SFTP I/O deliberately: the panel process runs as the
// unprivileged veil user with /etc/veil mounted read-only, so it can neither
// read the root-only destination secrets nor write them. The scheduled
// veil-backup.service runs the same uploads through the CLI path instead —
// it runs as root and reads the same config file directly, so both paths
// share internal/backupsftp without requiring the helper to be up.

// backupSftpEngine builds the destination engine for the resolved paths. The
// dial seam comes from ProductionConfig so tests substitute a fake RemoteFS;
// a nil SftpDial reads the package-level backupsftp.Dial at call time so a
// test can swap it after the executor was constructed.
func backupSftpEngine(config ProductionConfig, paths BackupSftpPaths) backupsftp.Engine {
	dial := config.SftpDial
	if dial == nil {
		dial = func(ctx context.Context, sftpConfig backupsftp.Config, knownHostsPath string) (backupsftp.RemoteFS, error) {
			return backupsftp.Dial(ctx, sftpConfig, knownHostsPath)
		}
	}
	return backupsftp.Engine{
		Paths: backupsftp.Paths{
			ConfigPath:     paths.ConfigPath,
			StatusPath:     paths.StatusPath,
			KnownHostsPath: paths.KnownHostsPath,
		},
		Dial: dial,
		Now:  config.Now,
	}
}

// applyRemoteUpload pushes a freshly created local archive to the configured
// SFTP destination. "Not configured" or "disabled" is a silent no-op; a real
// failure is recorded on the result (and the status file) without turning
// into a helper-level error, because the local archive is already committed.
func applyRemoteUpload(ctx context.Context, config ProductionConfig, request ResolvedBackup, name string, result *BackupResult) {
	if request.SftpPaths.ConfigPath == "" {
		return
	}
	engine := backupSftpEngine(config, request.SftpPaths)
	synced, configured, err := engine.SyncArchive(ctx, filepath.Join(request.BackupRoot, name), name, nil)
	if err != nil {
		result.RemoteError = err.Error()
		result.Warning = appendBackupResultWarning(result.Warning, "sftp upload failed: "+err.Error())
		return
	}
	if !configured || synced.Uploaded == nil {
		return
	}
	result.RemoteUpload = &BackupRemoteUpload{
		Archive: synced.Uploaded.Archive,
		Size:    synced.Uploaded.Size,
		SHA256:  synced.Uploaded.SHA256,
	}
}

// applyRemotePrune mirrors the local retention decision onto the remote
// listing after a successful local prune. Remote failures follow the same
// never-break-local contract as the upload.
func applyRemotePrune(ctx context.Context, config ProductionConfig, request ResolvedBackup, policy backup.RetentionPolicy, result *BackupResult) {
	if request.SftpPaths.ConfigPath == "" {
		return
	}
	engine := backupSftpEngine(config, request.SftpPaths)
	sftpConfig, err := engine.LoadConfig()
	if err != nil {
		result.RemoteError = err.Error()
		result.Warning = appendBackupResultWarning(result.Warning, "sftp remote prune failed: "+err.Error())
		return
	}
	if sftpConfig == nil || !sftpConfig.Enabled {
		return
	}
	pruned, err := engine.RemotePrune(ctx, *sftpConfig, policy)
	if err != nil {
		result.RemoteError = err.Error()
		result.RemotePruned = pruned.Deleted
		result.RemoteKept = pruned.Kept
		result.Warning = appendBackupResultWarning(result.Warning, "sftp remote prune failed: "+err.Error())
		return
	}
	result.RemotePruned = pruned.Deleted
	result.RemoteKept = pruned.Kept
}

func runProductionBackupSftp(ctx context.Context, config ProductionConfig, request ResolvedBackupSftp) (BackupSftpResult, error) {
	engine := backupSftpEngine(config, request.Paths)
	switch request.Action {
	case BackupSftpActionGet:
		return backupSftpGet(engine)
	case BackupSftpActionSet:
		return backupSftpSet(engine, request.Config)
	case BackupSftpActionDelete:
		if err := engine.DeleteConfig(); err != nil {
			return BackupSftpResult{}, err
		}
		return BackupSftpResult{
			Destination: &BackupSftpDestination{Configured: false},
			Status:      sftpStatusFromEngine(engine),
		}, nil
	case BackupSftpActionList, BackupSftpActionFetch:
		sftpConfig, err := requireSftpConfig(engine)
		if err != nil {
			return BackupSftpResult{}, err
		}
		if request.Action == BackupSftpActionList {
			entries, err := engine.RemoteList(ctx, *sftpConfig)
			if err != nil {
				return BackupSftpResult{}, err
			}
			result := BackupSftpResult{Archives: make([]BackupArchive, 0, len(entries))}
			for _, entry := range entries {
				result.Archives = append(result.Archives, BackupArchive{
					Name: entry.Name, Size: entry.Size,
					CreatedAt: entry.CreatedAt.UTC().Format(time.RFC3339), Encrypted: entry.Encrypted,
				})
			}
			return result, nil
		}
		entry, err := engine.FetchArchive(ctx, *sftpConfig, request.BackupRoot, request.ArchiveName)
		if err != nil {
			return BackupSftpResult{}, err
		}
		return BackupSftpResult{Archive: &BackupArchive{
			Name: entry.Name, Size: entry.Size,
			CreatedAt: entry.CreatedAt.UTC().Format(time.RFC3339), Encrypted: entry.Encrypted,
		}}, nil
	default:
		return BackupSftpResult{}, newError(ErrorInvalidRequest, "unsupported sftp backup action")
	}
}

// backupSftpGet renders the secret-free destination view plus the recorded
// remote-operation status. An absent config reports configured=false.
func backupSftpGet(engine backupsftp.Engine) (BackupSftpResult, error) {
	sftpConfig, err := engine.LoadConfig()
	if err != nil {
		return BackupSftpResult{}, err
	}
	result := BackupSftpResult{Status: sftpStatusFromEngine(engine)}
	if sftpConfig == nil {
		result.Destination = &BackupSftpDestination{Configured: false}
		return result, nil
	}
	destination := sftpDestinationFromView(sftpConfig.PublicView())
	result.Destination = &destination
	return result, nil
}

// backupSftpSet validates and persists the destination config. Secret fields
// are write-only: a nil pointer keeps the stored value, a non-nil pointer
// replaces it (empty clears). The previous config is loaded solely for that
// merge — nothing secret is ever returned.
func backupSftpSet(engine backupsftp.Engine, request *BackupSftpConfig) (BackupSftpResult, error) {
	if request == nil {
		return BackupSftpResult{}, newError(ErrorInvalidRequest, "sftp destination config is required")
	}
	merged := backupsftp.Config{
		Enabled:   request.Enabled,
		Host:      request.Host,
		Port:      request.Port,
		User:      request.User,
		RemoteDir: request.RemoteDir,
		AuthType:  request.AuthType,
		KeyPath:   request.KeyPath,
	}
	existing, err := engine.LoadConfig()
	if err != nil {
		return BackupSftpResult{}, err
	}
	if existing != nil {
		merged.Password = existing.Password
		merged.KeyPassphrase = existing.KeyPassphrase
		merged.HostKey = existing.HostKey
	}
	if request.Password != nil {
		merged.Password = *request.Password
	}
	if request.KeyPassphrase != nil {
		merged.KeyPassphrase = *request.KeyPassphrase
	}
	if request.HostKey != nil {
		merged.HostKey = *request.HostKey
	}
	// An auth-type switch retires the unused credential: keeping a stale
	// password after moving to key auth (or a passphrase after password auth)
	// leaves secret material on disk that can never be exercised again.
	switch merged.AuthType {
	case backupsftp.AuthTypeKey:
		merged.Password = ""
	case backupsftp.AuthTypePassword:
		merged.KeyPath = ""
		merged.KeyPassphrase = ""
	}
	if err := engine.SaveConfig(merged); err != nil {
		return BackupSftpResult{}, newError(ErrorInvalidRequest, err.Error())
	}
	destination := sftpDestinationFromView(merged.PublicView())
	return BackupSftpResult{
		Destination: &destination,
		Status:      sftpStatusFromEngine(engine),
	}, nil
}

// requireSftpConfig loads the destination for operations that need a live
// endpoint; an absent config is a clean invalid_request rather than a dial
// error.
func requireSftpConfig(engine backupsftp.Engine) (*backupsftp.Config, error) {
	sftpConfig, err := engine.LoadConfig()
	if err != nil {
		return nil, err
	}
	if sftpConfig == nil {
		return nil, newError(ErrorInvalidRequest, "sftp destination is not configured")
	}
	return sftpConfig, nil
}

func sftpDestinationFromView(view backupsftp.View) BackupSftpDestination {
	return BackupSftpDestination{
		Configured:       view.Configured,
		Enabled:          view.Enabled,
		Host:             view.Host,
		Port:             view.Port,
		User:             view.User,
		RemoteDir:        view.RemoteDir,
		AuthType:         view.AuthType,
		KeyPath:          view.KeyPath,
		PasswordSet:      view.PasswordSet,
		KeyPassphraseSet: view.KeyPassphraseSet,
		HostKeySet:       view.HostKeySet,
	}
}

func sftpStatusFromEngine(engine backupsftp.Engine) *BackupSftpStatus {
	status := engine.Status()
	return &BackupSftpStatus{
		LastUploadAt:      status.LastUploadAt,
		LastUploadArchive: status.LastUploadArchive,
		LastFetchAt:       status.LastFetchAt,
		LastFetchArchive:  status.LastFetchArchive,
		LastPruneAt:       status.LastPruneAt,
		LastError:         status.LastError,
		LastErrorAt:       status.LastErrorAt,
	}
}
