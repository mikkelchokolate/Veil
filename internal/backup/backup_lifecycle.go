package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Test hooks for lifecycle operations that are hard to trigger via the filesystem.
var (
	lifecycleMkdirAll     = os.MkdirAll
	lifecycleManifestSave = func(path string, manifest Manifest) error {
		return NewBackupManifestStore(path).Save(manifest)
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

	backupPath := filepath.Join(l.Dir, backupID)
	if err := lifecycleMkdirAll(backupPath, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}

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
		dst := filepath.Join(backupPath, member)

		if err := copyFile(src, dst, srcInfo.Mode()); err != nil {
			return "", fmt.Errorf("backup %s: %w", src, err)
		}

		manifest.Entries = append(manifest.Entries, BackupEntry{
			OriginalPath: key,
			BackupPath:   member,
			Size:         srcInfo.Size(),
		})
	}

	manifestPath := filepath.Join(backupPath, backupManifestName)
	if err := lifecycleManifestSave(manifestPath, manifest); err != nil {
		return "", err
	}

	return backupID, nil
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
