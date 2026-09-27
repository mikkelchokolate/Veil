package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

// Test hooks for lifecycle operations that are hard to trigger via the filesystem.
var (
	lifecycleMkdirAll = os.MkdirAll
	// lifecycleManifestSave writes manifest.json inside the pinned backup
	// directory via a temp+rename — never through a re-resolved path, because
	// the backup root lives under a service-account-owned tree (#1131).
	lifecycleManifestSave = func(dir *safefs.Dir, manifest Manifest) error {
		return saveManifestInDir(dir, manifest)
	}
	restoreCommitRename = os.Rename
)

type Lifecycle struct {
	Dir string
}

type BackupLifecycle = Lifecycle

func NewLifecycle(dir string) Lifecycle {
	return Lifecycle{Dir: dir}
}

func NewBackupLifecycle(dir string) BackupLifecycle {
	return NewLifecycle(dir)
}

func (l Lifecycle) BackupExisting(paths []string) (string, error) {
	backupID, err := NewBackupIDPolicy(time.Now, backupPathExists).Next(l.Dir)
	if err != nil {
		return "", err
	}

	// l.Dir typically sits inside the service-owned /var/lib/veil, so a plain
	// MkdirAll on <l.Dir>/<backupID> follows a "backups" component swapped
	// for a symlink — root would create directories and drop file copies
	// under an attacker-chosen target (#1131). Pin the parent, create or
	// verify the root descriptor-relative (a symlinked leaf fails ELOOP),
	// and keep every member and manifest write under the pinned backup dir.
	backupDir, err := l.openPinnedBackupDir(backupID)
	if err != nil {
		return "", err
	}
	defer backupDir.Close()

	manifest := backupManifest{}
	seen := make(map[string]struct{})
	memberIndex := 0

	for _, src := range paths {
		srcInfo, err := os.Stat(src)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("stat %s: %w", src, err)
		}
		if srcInfo.IsDir() {
			continue
		}

		key, err := filepath.Abs(src)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", src, err)
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		member := backupMemberName(memberIndex, src)
		memberIndex++

		if err := copyFileIntoPinnedDir(backupDir, src, member, srcInfo.Mode()); err != nil {
			return "", fmt.Errorf("backup %s: %w", src, err)
		}

		manifest.Entries = append(manifest.Entries, BackupEntry{
			OriginalPath: key,
			BackupPath:   member,
			Size:         srcInfo.Size(),
		})
	}

	if err := lifecycleManifestSave(backupDir, manifest); err != nil {
		return "", err
	}

	return backupID, nil
}

// openPinnedBackupDir creates (if needed) and opens <l.Dir>/<backupID> as a
// pinned directory handle. The parent is opened follow-symlinks — it is the
// caller-configured root — but the backups leaf and the per-backup dir are
// created and opened descriptor-relative with O_NOFOLLOW, so neither can be a
// symlink planted by the service identity that owns the parent (#1131).
func (l Lifecycle) openPinnedBackupDir(backupID string) (*safefs.Dir, error) {
	parent := filepath.Dir(l.Dir)
	leaf := filepath.Base(l.Dir)
	if err := lifecycleMkdirAll(parent, 0o700); err != nil {
		return nil, fmt.Errorf("create backup directory: %w", err)
	}
	parentDir, err := safefs.OpenDirFollow(parent)
	if err != nil {
		return nil, fmt.Errorf("open backup parent directory: %w", err)
	}
	defer parentDir.Close()
	if _, err := parentDir.StatAt(leaf); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat backup root: %w", err)
		}
		if err := parentDir.MkdirAt(leaf, 0o700); err != nil {
			return nil, fmt.Errorf("create backup directory: %w", err)
		}
	}
	rootDir, err := parentDir.OpenDirAt(leaf)
	if err != nil {
		// A swapped-in symlink fails ELOOP here instead of being followed.
		return nil, fmt.Errorf("open backup root: %w", err)
	}
	defer rootDir.Close()
	if err := rootDir.MkdirAt(backupID, 0o700); err != nil {
		return nil, fmt.Errorf("create backup directory: %w", err)
	}
	backupDir, err := rootDir.OpenDirAt(backupID)
	if err != nil {
		return nil, fmt.Errorf("open backup directory: %w", err)
	}
	return backupDir, nil
}

// copyFileIntoPinnedDir copies src into dir as name via temp+rename, never
// resolving the destination by path. The source is opened
// O_NOFOLLOW|O_NONBLOCK and fstat-checked, so a managed file swapped for a
// symlink is refused rather than followed and a swapped FIFO cannot block
// the copy (#1083, #1131).
func copyFileIntoPinnedDir(dir *safefs.Dir, src, name string, mode os.FileMode) error {
	srcFile, err := safefs.OpenNoFollow(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()
	srcStat, err := srcFile.Stat()
	if err != nil {
		return err
	}
	if !srcStat.Mode().IsRegular() {
		return fmt.Errorf("backup source %s is not a regular file", src)
	}

	tmp, tmpName, err := dir.CreateTempAt(".veil-copy-", 0o600)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = dir.RemoveAt(tmpName)
		}
	}()

	if _, err := fileCopierCopy(tmp, srcFile); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if err := tmp.Chmod(mode.Perm()); err != nil {
		return err
	}
	if err := fileCopierSync(tmp); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := dir.RenameAt(tmpName, name); err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	committed = true
	return dir.File().Sync()
}

// saveManifestInDir writes manifest.json inside the pinned backup directory
// with the same write-temp/fsync/rename/dirsync ordering atomicfile.Write
// uses, but never re-resolving a path under the service-influenced tree.
func saveManifestInDir(dir *safefs.Dir, manifest Manifest) error {
	manifestData, err := manifestMarshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	tmp, tmpName, err := dir.CreateTempAt(".veil-manifest-", 0o600)
	if err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = dir.RemoveAt(tmpName)
		}
	}()
	if _, err := tmp.Write(manifestData); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := dir.RenameAt(tmpName, backupManifestName); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	committed = true
	return dir.File().Sync()
}

func (l Lifecycle) Restore(backupID string) ([]string, error) {
	backupPath, _, err := l.resolveBackupDir(backupID)
	if err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(backupPath, backupManifestName)
	manifest, err := NewBackupManifestStore(manifestPath).Load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("manifest not found in backup %s", backupID)
		}
		return nil, err
	}

	entries, err := resolveManifestEntries(backupPath, manifest)
	if err != nil {
		return nil, err
	}

	existingPaths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if _, err := os.Lstat(entry.OriginalPath); err == nil {
			existingPaths = append(existingPaths, entry.OriginalPath)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat %s: %w", entry.OriginalPath, err)
		}
	}
	var safetyID string
	if len(existingPaths) > 0 {
		id, safetyErr := l.BackupExisting(existingPaths)
		if safetyErr != nil {
			return nil, fmt.Errorf("create safety backup before restore: %w", safetyErr)
		}
		safetyID = id
	}

	type stagedRestore struct {
		dest   string
		staged string
	}
	staged := make([]stagedRestore, 0, len(entries))
	defer func() {
		for _, item := range staged {
			if item.staged != "" {
				_ = os.Remove(item.staged)
			}
		}
	}()

	for _, entry := range entries {
		parentDir := filepath.Dir(entry.OriginalPath)
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			return nil, restoreErr(safetyID, fmt.Sprintf("create parent dir %s", parentDir), err)
		}
		tmp, err := os.CreateTemp(parentDir, ".veil-restore-*")
		if err != nil {
			return nil, restoreErr(safetyID, fmt.Sprintf("stage %s", entry.OriginalPath), err)
		}
		tmpPath := tmp.Name()
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return nil, restoreErr(safetyID, fmt.Sprintf("stage %s", entry.OriginalPath), err)
		}
		if err := copyFile(entry.BackupPath, tmpPath, entry.Mode); err != nil {
			_ = os.Remove(tmpPath)
			return nil, restoreErr(safetyID, fmt.Sprintf("restore %s", entry.OriginalPath), err)
		}
		if existing, err := os.Lstat(entry.OriginalPath); err == nil {
			if err := restoreChownToMatch(tmpPath, existing); err != nil {
				_ = os.Remove(tmpPath)
				return nil, restoreErr(safetyID, fmt.Sprintf("restore %s", entry.OriginalPath), err)
			}
		} else if !os.IsNotExist(err) {
			_ = os.Remove(tmpPath)
			return nil, restoreErr(safetyID, fmt.Sprintf("stat %s", entry.OriginalPath), err)
		}
		staged = append(staged, stagedRestore{dest: entry.OriginalPath, staged: tmpPath})
	}

	var restored []string
	for i, item := range staged {
		if err := restoreCommitRename(item.staged, item.dest); err != nil {
			rollbackErr := l.rollbackRestoredFiles(safetyID, restored)
			if rollbackErr != nil {
				return nil, fmt.Errorf("restore %s: %v (safety backup %s); restore safety backup: %w", item.dest, err, safetyID, rollbackErr)
			}
			return nil, restoreErr(safetyID, fmt.Sprintf("restore %s", item.dest), err)
		}
		staged[i].staged = ""
		restored = append(restored, item.dest)
	}
	return restored, nil
}

func restoreErr(safetyID, op string, err error) error {
	if safetyID == "" {
		return fmt.Errorf("%s: %w", op, err)
	}
	return fmt.Errorf("%s: %w (safety backup %s)", op, err, safetyID)
}

func (l Lifecycle) rollbackRestoredFiles(safetyID string, restored []string) error {
	if len(restored) == 0 {
		return nil
	}
	if safetyID == "" {
		var first error
		for _, path := range restored {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) && first == nil {
				first = err
			}
		}
		return first
	}
	safetyPath, _, err := l.resolveBackupDir(safetyID)
	if err != nil {
		return err
	}
	manifest, err := NewBackupManifestStore(filepath.Join(safetyPath, backupManifestName)).Load()
	if err != nil {
		return err
	}
	entries, err := resolveManifestEntries(safetyPath, manifest)
	if err != nil {
		return err
	}
	byOrig := make(map[string]resolvedRestoreEntry, len(entries))
	for _, entry := range entries {
		byOrig[entry.OriginalPath] = entry
	}
	for _, path := range restored {
		entry, ok := byOrig[path]
		if !ok {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if err := copyFile(entry.BackupPath, entry.OriginalPath, entry.Mode); err != nil {
			return err
		}
	}
	return nil
}

func (l Lifecycle) Cleanup(backupID string) error {
	backupPath, _, err := l.resolveBackupDir(backupID)
	if err != nil {
		return err
	}
	return os.RemoveAll(backupPath)
}

func (l Lifecycle) resolveBackupDir(backupID string) (string, os.FileInfo, error) {
	if err := validateBackupID(backupID); err != nil {
		return "", nil, err
	}
	root, err := filepath.Abs(l.Dir)
	if err != nil {
		return "", nil, fmt.Errorf("resolve backup dir: %w", err)
	}
	candidate := filepath.Join(root, backupID)
	if !backupPathWithin(root, candidate) {
		return "", nil, fmt.Errorf("invalid backup ID %q", backupID)
	}
	info, err := os.Lstat(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, fmt.Errorf("backup %s does not exist in %s", backupID, l.Dir)
		}
		return "", nil, fmt.Errorf("stat backup dir: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", nil, fmt.Errorf("backup %s is not a directory", backupID)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", nil, fmt.Errorf("stat backup dir: %w", err)
	}
	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", nil, fmt.Errorf("stat backup dir: %w", err)
	}
	if !backupPathWithin(resolvedRoot, resolvedCandidate) {
		return "", nil, fmt.Errorf("invalid backup ID %q", backupID)
	}
	return candidate, info, nil
}

func (l Lifecycle) List() ([]string, error) {
	entries, err := os.ReadDir(l.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read backup dir: %w", err)
	}

	var ids []string
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}

	sort.Strings(ids)
	return ids, nil
}

func backupMemberName(index int, src string) string {
	base := filepath.Base(filepath.Clean(src))
	if base == "." || base == ".." || base == string(filepath.Separator) || !filepath.IsLocal(base) {
		base = "member"
	}
	return fmt.Sprintf("%d_%s", index, base)
}
