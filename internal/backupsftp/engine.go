package backupsftp

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/backup"
)

// Paths locates the on-disk state an SFTP destination needs. ConfigPath is
// the root-only secrets file under the etc dir; StatusPath and
// KnownHostsPath live under the state dir so the scheduled backup unit —
// which mounts /var/lib/veil writable but keeps /etc read-only — can record
// outcomes and complete trust-on-first-use without relaxing its filesystem
// confinement. InstallIDPath persists the random per-installation identity
// that namespaces this node's remote archives (#1184); when empty it falls
// back to a backup-sftp.install-id file beside the status file.
type Paths struct {
	ConfigPath     string
	StatusPath     string
	KnownHostsPath string
	InstallIDPath  string
}

// Engine binds the destination paths to the dial and clock seams so the
// privileged helper (socket-driven operations) and the scheduled backup CLI
// (which runs without the helper) share one orchestration path for
// upload/list/fetch/prune and the status bookkeeping around them.
type Engine struct {
	Paths Paths
	// Dial connects to the configured endpoint; nil uses the package-level
	// real SSH/SFTP dialer.
	Dial Dialer
	Now  func() time.Time
}

func (e Engine) dialer() Dialer {
	if e.Dial != nil {
		return e.Dial
	}
	return Dial
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e Engine) LoadConfig() (*Config, error) {
	if e.Paths.ConfigPath == "" {
		return nil, nil
	}
	return LoadConfig(e.Paths.ConfigPath)
}

func (e Engine) SaveConfig(config Config) error {
	return SaveConfig(e.Paths.ConfigPath, config)
}

func (e Engine) DeleteConfig() error {
	return DeleteConfig(e.Paths.ConfigPath)
}

// Status returns the recorded remote-operation outcomes; a missing or
// unreadable status file reports empty rather than erroring.
func (e Engine) Status() Status {
	if e.Paths.StatusPath == "" {
		return Status{}
	}
	return LoadStatus(e.Paths.StatusPath)
}

// recordStatus merges one status mutation best-effort: status bookkeeping is
// observability, so a write failure must never fail or shadow the remote
// operation it describes.
func (e Engine) recordStatus(mutate func(*Status)) {
	if e.Paths.StatusPath == "" {
		return
	}
	status := LoadStatus(e.Paths.StatusPath)
	mutate(&status)
	_ = SaveStatus(e.Paths.StatusPath, status)
}

// recordError stores the operation failure so the status endpoint reports a
// lastError even when the caller only propagates the error upstream.
func (e Engine) recordError(err error) {
	e.recordStatus(func(status *Status) {
		status.LastError = err.Error()
		status.LastErrorAt = e.now().UTC().Format(time.RFC3339)
	})
}

func (e Engine) connect(ctx context.Context, config Config) (RemoteFS, error) {
	return e.dialer()(ctx, config, e.Paths.KnownHostsPath)
}

// namespacedConfig returns config with RemoteDir scoped to this
// installation's subdirectory. Remote archives live under
// <remoteDir>/veil-node-<install-id>/ so nodes sharing one destination never
// list, fetch, or prune each other's archives — and same-second archive names
// can never collide cross-node (#1184). The install id is persisted locally
// on first use. Archives written before namespacing existed sit unscoped at
// the remoteDir root: they are orphans the engine deliberately never lists,
// fetches, or removes.
func (e Engine) namespacedConfig(config Config) (Config, error) {
	idPath := e.Paths.InstallIDPath
	if idPath == "" && e.Paths.StatusPath != "" {
		idPath = filepath.Join(filepath.Dir(e.Paths.StatusPath), InstallIDFileName)
	}
	if idPath == "" {
		return Config{}, errors.New("sftp install id path is not configured; the remote namespace cannot be resolved")
	}
	id, err := loadOrCreateInstallID(idPath)
	if err != nil {
		return Config{}, err
	}
	scoped := config
	scoped.RemoteDir = path.Join(config.RemoteDir, namespaceDir(id))
	return scoped, nil
}

// SyncResult reports what one remote synchronization achieved.
type SyncResult struct {
	Uploaded *UploadReceipt `json:"uploaded,omitempty"`
	// Pruned/Kept describe the remote retention run when a policy was given.
	Pruned []string `json:"pruned,omitempty"`
	Kept   []string `json:"kept,omitempty"`
}

// SyncArchive is the post-create hook shared by the privileged helper's
// backup_create handling and the scheduled veil-backup.service CLI path: it
// uploads the freshly written archive to this installation's remote
// namespace and, when a retention policy is supplied, mirrors the
// daily/weekly/monthly prune on the remote listing. configured is false when
// no destination is on file or it is disabled, so callers distinguish
// "nothing to do" from a real skip.
//
// A plaintext archive (a name without the .enc suffix) is refused with
// ErrUnencryptedArchive before the remote is even dialed — remote storage is
// an encrypted-only tier, so --allow-unencrypted stays a purely local opt-in
// (#1188). Upload additionally verifies the encrypted-archive magic.
//
// Failures return the error — callers decide how loud to make it — while the
// status file records the same outcome for the status endpoint.
func (e Engine) SyncArchive(ctx context.Context, localPath, name string, policy *backup.RetentionPolicy) (result SyncResult, configured bool, err error) {
	config, err := e.LoadConfig()
	if err != nil {
		e.recordError(err)
		return SyncResult{}, false, err
	}
	if config == nil || !config.Enabled {
		return SyncResult{}, false, nil
	}
	if !strings.HasSuffix(strings.ToLower(name), ".enc") {
		err := fmt.Errorf("%w: %q", ErrUnencryptedArchive, name)
		e.recordError(err)
		return SyncResult{}, true, err
	}
	scoped, err := e.namespacedConfig(*config)
	if err != nil {
		e.recordError(err)
		return SyncResult{}, true, err
	}
	remote, err := e.connect(ctx, scoped)
	if err != nil {
		e.recordError(err)
		return SyncResult{}, true, err
	}
	defer remote.Close()
	receipt, err := Upload(ctx, remote, scoped, localPath, name)
	if err != nil {
		e.recordError(err)
		return SyncResult{}, true, err
	}
	result.Uploaded = &receipt
	now := e.now().UTC().Format(time.RFC3339)
	e.recordStatus(func(status *Status) {
		status.LastUploadAt = now
		status.LastUploadArchive = receipt.Archive
		status.LastError, status.LastErrorAt = "", ""
	})
	if policy != nil {
		pruned, pruneErr := Prune(ctx, remote, scoped, *policy)
		if pruneErr != nil {
			e.recordError(pruneErr)
			result.Pruned = pruned.Deleted
			result.Kept = pruned.Kept
			return result, true, pruneErr
		}
		result.Pruned = pruned.Deleted
		result.Kept = pruned.Kept
		e.recordStatus(func(status *Status) {
			status.LastPruneAt = e.now().UTC().Format(time.RFC3339)
			status.LastError, status.LastErrorAt = "", ""
		})
	}
	return result, true, nil
}

// RemotePruneIfConfigured mirrors a retention policy onto the remote listing
// when a destination is configured and enabled; attempted is false when
// there is nothing to do.
func (e Engine) RemotePruneIfConfigured(ctx context.Context, policy backup.RetentionPolicy) (result backup.PruneResult, attempted bool, err error) {
	config, err := e.LoadConfig()
	if err != nil {
		e.recordError(err)
		return backup.PruneResult{}, false, err
	}
	if config == nil || !config.Enabled {
		return backup.PruneResult{}, false, nil
	}
	result, err = e.RemotePrune(ctx, *config, policy)
	return result, true, err
}

// RemoteList returns the managed archives in this installation's remote
// namespace and records dial/list failures in the status file. Archives other
// nodes or older unscoped uploads left in remoteDir are not shown.
func (e Engine) RemoteList(ctx context.Context, config Config) ([]backup.ArchiveEntry, error) {
	scoped, err := e.namespacedConfig(config)
	if err != nil {
		e.recordError(err)
		return nil, err
	}
	remote, err := e.connect(ctx, scoped)
	if err != nil {
		e.recordError(err)
		return nil, err
	}
	defer remote.Close()
	entries, err := List(ctx, remote, scoped)
	if err != nil {
		e.recordError(err)
		return nil, err
	}
	return entries, nil
}

// RemotePrune applies the shared retention decision to this installation's
// remote namespace and records the outcome. Foreign archives — other nodes'
// namespaces and pre-namespacing orphans at the remoteDir root — are never
// candidates for removal (#1184).
func (e Engine) RemotePrune(ctx context.Context, config Config, policy backup.RetentionPolicy) (backup.PruneResult, error) {
	scoped, err := e.namespacedConfig(config)
	if err != nil {
		e.recordError(err)
		return backup.PruneResult{}, err
	}
	remote, err := e.connect(ctx, scoped)
	if err != nil {
		e.recordError(err)
		return backup.PruneResult{}, err
	}
	defer remote.Close()
	result, err := Prune(ctx, remote, scoped, policy)
	if err != nil {
		e.recordError(err)
		return result, err
	}
	e.recordStatus(func(status *Status) {
		status.LastPruneAt = e.now().UTC().Format(time.RFC3339)
		status.LastError, status.LastErrorAt = "", ""
	})
	return result, nil
}

// FetchArchive downloads one archive from this installation's remote
// namespace into localDir and records the outcome in the status file.
func (e Engine) FetchArchive(ctx context.Context, config Config, localDir, name string) (backup.ArchiveEntry, error) {
	scoped, err := e.namespacedConfig(config)
	if err != nil {
		e.recordError(err)
		return backup.ArchiveEntry{}, err
	}
	remote, err := e.connect(ctx, scoped)
	if err != nil {
		e.recordError(err)
		return backup.ArchiveEntry{}, err
	}
	defer remote.Close()
	entry, err := Fetch(ctx, remote, scoped, localDir, name)
	if err != nil {
		e.recordError(err)
		return backup.ArchiveEntry{}, err
	}
	e.recordStatus(func(status *Status) {
		status.LastFetchAt = e.now().UTC().Format(time.RFC3339)
		status.LastFetchArchive = entry.Name
		status.LastError, status.LastErrorAt = "", ""
	})
	return entry, nil
}
