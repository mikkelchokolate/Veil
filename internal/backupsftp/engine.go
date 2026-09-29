package backupsftp

import (
	"context"
	"time"

	"github.com/mikkelchokolate/Veil/internal/backup"
)

// Paths locates the on-disk state an SFTP destination needs. ConfigPath is
// the root-only secrets file under the etc dir; StatusPath and
// KnownHostsPath live under the state dir so the scheduled backup unit —
// which mounts /var/lib/veil writable but keeps /etc read-only — can record
// outcomes and complete trust-on-first-use without relaxing its filesystem
// confinement.
type Paths struct {
	ConfigPath     string
	StatusPath     string
	KnownHostsPath string
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

// SyncResult reports what one remote synchronization achieved.
type SyncResult struct {
	Uploaded *UploadReceipt `json:"uploaded,omitempty"`
	// Pruned/Kept describe the remote retention run when a policy was given.
	Pruned []string `json:"pruned,omitempty"`
	Kept   []string `json:"kept,omitempty"`
}

// SyncArchive is the post-create hook shared by the privileged helper's
// backup_create handling and the scheduled veil-backup.service CLI path: it
// uploads the freshly written archive to the remote directory and, when a
// retention policy is supplied, mirrors the daily/weekly/monthly prune on
// the remote listing. configured is false when no destination is on file or
// it is disabled, so callers distinguish "nothing to do" from a real skip.
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
	remote, err := e.connect(ctx, *config)
	if err != nil {
		e.recordError(err)
		return SyncResult{}, true, err
	}
	defer remote.Close()
	receipt, err := Upload(ctx, remote, *config, localPath, name)
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
		pruned, pruneErr := Prune(ctx, remote, *config, *policy)
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

// RemoteList returns the managed archives in the remote directory and
// records dial/list failures in the status file.
func (e Engine) RemoteList(ctx context.Context, config Config) ([]backup.ArchiveEntry, error) {
	remote, err := e.connect(ctx, config)
	if err != nil {
		e.recordError(err)
		return nil, err
	}
	defer remote.Close()
	entries, err := List(ctx, remote, config)
	if err != nil {
		e.recordError(err)
		return nil, err
	}
	return entries, nil
}

// RemotePrune applies the shared retention decision to the remote listing
// and records the outcome.
func (e Engine) RemotePrune(ctx context.Context, config Config, policy backup.RetentionPolicy) (backup.PruneResult, error) {
	remote, err := e.connect(ctx, config)
	if err != nil {
		e.recordError(err)
		return backup.PruneResult{}, err
	}
	defer remote.Close()
	result, err := Prune(ctx, remote, config, policy)
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

// FetchArchive downloads one remote archive into localDir and records the
// outcome in the status file.
func (e Engine) FetchArchive(ctx context.Context, config Config, localDir, name string) (backup.ArchiveEntry, error) {
	remote, err := e.connect(ctx, config)
	if err != nil {
		e.recordError(err)
		return backup.ArchiveEntry{}, err
	}
	defer remote.Close()
	entry, err := Fetch(ctx, remote, config, localDir, name)
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
