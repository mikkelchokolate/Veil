package backup

import (
	"encoding/json"
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
	// restoreCommitRename renames oldLeaf to newLeaf inside an already-pinned
	// destination directory — never a re-resolved path (#1219).
	restoreCommitRename = func(dir *safefs.Dir, oldLeaf, newLeaf string) error {
		return dir.RenameAt(oldLeaf, newLeaf)
	}
)

type Lifecycle struct {
	Dir string
	// RestoreRoots bounds manifest-declared restore destinations: when
	// non-empty, every entry's OriginalPath must live under one of these
	// roots (checked lexically and, for existing leaves, through
	// EvalSymlinks), so a tampered manifest cannot aim a privileged restore
	// at an attacker-chosen path (#1219). nil keeps legacy behavior for
	// callers that restore into caller-controlled test roots.
	RestoreRoots []string
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
	backupDir, absRoot, err := l.openExistingBackupDir(backupID)
	if err != nil {
		return nil, err
	}
	defer backupDir.Close()

	manifest, err := loadManifestInDir(backupDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("manifest not found in backup %s", backupID)
		}
		return nil, err
	}

	allowedRoots, err := l.resolvedRestoreRoots()
	if err != nil {
		return nil, err
	}
	entries, err := resolveManifestEntries(backupDir, absRoot, manifest, allowedRoots)
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

	// Parent destination directories are pinned once for the whole restore:
	// staging, metadata preservation and the final rename are all resolved
	// descriptor-relative, so a destination dir (or leaf) swapped mid-flight
	// redirects nothing (#1219).
	parentDirs := map[string]*safefs.Dir{}
	defer func() {
		for _, dir := range parentDirs {
			_ = dir.Close()
		}
	}()
	pinParent := func(parentPath string) (*safefs.Dir, error) {
		if dir, ok := parentDirs[parentPath]; ok {
			return dir, nil
		}
		dir, err := safefs.OpenDir(parentPath)
		if err != nil {
			return nil, err
		}
		parentDirs[parentPath] = dir
		return dir, nil
	}

	type stagedRestore struct {
		dest    string
		parent  *safefs.Dir
		tmpLeaf string
	}
	staged := make([]stagedRestore, 0, len(entries))
	defer func() {
		for _, item := range staged {
			if item.tmpLeaf != "" {
				_ = item.parent.RemoveAt(item.tmpLeaf)
			}
		}
	}()

	for _, entry := range entries {
		parentPath := filepath.Dir(entry.OriginalPath)
		if err := os.MkdirAll(parentPath, 0o755); err != nil {
			return nil, restoreErr(safetyID, fmt.Sprintf("create parent dir %s", parentPath), err)
		}
		parent, err := pinParent(parentPath)
		if err != nil {
			return nil, restoreErr(safetyID, fmt.Sprintf("open parent dir %s", parentPath), err)
		}
		tmpLeaf, err := stageManifestEntry(backupDir, entry, parent, filepath.Base(entry.OriginalPath))
		if err != nil {
			return nil, restoreErr(safetyID, fmt.Sprintf("restore %s", entry.OriginalPath), err)
		}
		staged = append(staged, stagedRestore{dest: entry.OriginalPath, parent: parent, tmpLeaf: tmpLeaf})
	}

	var restored []string
	for i, item := range staged {
		destLeaf := filepath.Base(item.dest)
		if err := restoreCommitRename(item.parent, item.tmpLeaf, destLeaf); err != nil {
			rollbackErr := l.rollbackRestoredFiles(safetyID, restored)
			if rollbackErr != nil {
				return nil, fmt.Errorf("restore %s: %v (safety backup %s); restore safety backup: %w", item.dest, err, safetyID, rollbackErr)
			}
			return nil, restoreErr(safetyID, fmt.Sprintf("restore %s", item.dest), err)
		}
		_ = item.parent.File().Sync()
		staged[i].tmpLeaf = ""
		restored = append(restored, item.dest)
	}
	return restored, nil
}

// stageManifestEntry streams one validated member into a fresh temp inside
// the pinned destination directory, applies mode (and prior ownership where
// a regular destination exists) on the open descriptor, and returns the temp
// leaf ready for a descriptor-relative rename (#1219).
func stageManifestEntry(backupDir *safefs.Dir, entry resolvedRestoreEntry, parent *safefs.Dir, destLeaf string) (string, error) {
	member, err := openBackupMemberAt(backupDir, entry.BackupPath)
	if err != nil {
		return "", err
	}
	defer member.Close()
	tmp, tmpLeaf, err := parent.CreateTempAt(".veil-restore-", 0o600)
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		_ = tmp.Close()
		_ = parent.RemoveAt(tmpLeaf)
		return "", err
	}
	if _, err := fileCopierCopy(tmp, member); err != nil {
		return fail(fmt.Errorf("copy: %w", err))
	}
	if err := tmp.Chmod(entry.Mode.Perm()); err != nil {
		return fail(err)
	}
	// Ownership must come from a stat of the destination leaf resolved
	// relative to the pinned parent — a lstat-by-path could observe a swapped
	// entry between check and chown (#1219).
	if existing, err := parent.StatAt(destLeaf); err == nil {
		if !existing.Mode().IsRegular() || existing.Mode()&os.ModeSymlink != 0 {
			return fail(fmt.Errorf("restore destination is not a regular file: %s", filepath.Join(parent.Path(), destLeaf)))
		}
		uid, gid := fileOwnerIDs(existing)
		if err := restoreChownToMatch(tmp, uid, gid); err != nil {
			return fail(err)
		}
	} else if !os.IsNotExist(err) {
		return fail(err)
	}
	if err := fileCopierSync(tmp); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = parent.RemoveAt(tmpLeaf)
		return "", err
	}
	return tmpLeaf, nil
}

// loadManifestInDir reads manifest.json through the pinned backup directory
// descriptor, never by re-resolving a path under the backup root (#1219).
func loadManifestInDir(dir *safefs.Dir) (Manifest, error) {
	file, err := dir.OpenFileAt(backupManifestName)
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Manifest{}, err
	}
	if !info.Mode().IsRegular() {
		return Manifest{}, fmt.Errorf("backup manifest is not a regular file")
	}
	var manifest Manifest
	if err := json.NewDecoder(file).Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return manifest, nil
}

// resolvedRestoreRoots absolutises the configured managed-root allowlist.
func (l Lifecycle) resolvedRestoreRoots() ([]string, error) {
	if len(l.RestoreRoots) == 0 {
		return nil, nil
	}
	roots := make([]string, 0, len(l.RestoreRoots))
	for _, root := range l.RestoreRoots {
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve restore root %s: %w", root, err)
		}
		roots = append(roots, abs)
	}
	return roots, nil
}

// openExistingBackupDir opens <l.Dir>/<backupID> as a pinned directory
// handle, requiring every checked component to be a real directory: the
// backups root leaf and the backup dir leaf are opened O_NOFOLLOW relative
// to their parent, so a swapped symlink fails instead of being followed into
// read or delete operations (#1219). Returns the pinned handle plus the
// display path used for member-validation error messages.
func (l Lifecycle) openExistingBackupDir(backupID string) (*safefs.Dir, string, error) {
	if err := validateBackupID(backupID); err != nil {
		return nil, "", err
	}
	root, err := filepath.Abs(l.Dir)
	if err != nil {
		return nil, "", fmt.Errorf("resolve backup dir: %w", err)
	}
	parentDir, err := safefs.OpenDirFollow(filepath.Dir(root))
	if err != nil {
		return nil, "", fmt.Errorf("open backup parent directory: %w", err)
	}
	defer parentDir.Close()
	rootDir, err := parentDir.OpenDirAt(filepath.Base(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("backup %s does not exist in %s", backupID, l.Dir)
		}
		return nil, "", fmt.Errorf("open backup root: %w", err)
	}
	defer rootDir.Close()
	info, err := rootDir.StatAt(backupID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", fmt.Errorf("backup %s does not exist in %s", backupID, l.Dir)
		}
		return nil, "", fmt.Errorf("stat backup dir: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, "", fmt.Errorf("backup %s is not a directory", backupID)
	}
	backupDir, err := rootDir.OpenDirAt(backupID)
	if err != nil {
		return nil, "", fmt.Errorf("open backup directory: %w", err)
	}
	return backupDir, filepath.Join(root, backupID), nil
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
		// No safety backup: only remove destinations that sit under the
		// configured managed roots — a manifest path is never trusted enough
		// to delete outside them (#1219).
		var first error
		for _, path := range restored {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) && first == nil {
				first = err
			}
		}
		return first
	}
	safetyDir, absRoot, err := l.openExistingBackupDir(safetyID)
	if err != nil {
		return err
	}
	defer safetyDir.Close()
	manifest, err := loadManifestInDir(safetyDir)
	if err != nil {
		return err
	}
	allowedRoots, err := l.resolvedRestoreRoots()
	if err != nil {
		return err
	}
	entries, err := resolveManifestEntries(safetyDir, absRoot, manifest, allowedRoots)
	if err != nil {
		return err
	}
	byOrig := make(map[string]resolvedRestoreEntry, len(entries))
	for _, entry := range entries {
		byOrig[entry.OriginalPath] = entry
	}
	for _, path := range restored {
		entry, ok := byOrig[path]
		parent, err := safefs.OpenDir(filepath.Dir(path))
		if err != nil {
			return err
		}
		if !ok {
			// Not in the safety manifest — remove only the leaf relative to
			// the pinned parent so a swapped parent can redirect nothing.
			err := parent.RemoveAt(filepath.Base(path))
			parent.Close()
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		err = copyBackupMemberToDest(safetyDir, entry, parent, filepath.Base(path))
		parent.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// copyBackupMemberToDest streams a pinned backup member into destLeaf inside
// the pinned destination directory via temp+rename, applying mode/ownership
// on the open temp descriptor (#1219).
func copyBackupMemberToDest(backupDir *safefs.Dir, entry resolvedRestoreEntry, destDir *safefs.Dir, destLeaf string) error {
	tmpLeaf, err := stageManifestEntry(backupDir, entry, destDir, destLeaf)
	if err != nil {
		return err
	}
	if err := destDir.RenameAt(tmpLeaf, destLeaf); err != nil {
		_ = destDir.RemoveAt(tmpLeaf)
		return err
	}
	return destDir.File().Sync()
}

func (l Lifecycle) Cleanup(backupID string) error {
	if err := validateBackupID(backupID); err != nil {
		return err
	}
	root, err := filepath.Abs(l.Dir)
	if err != nil {
		return fmt.Errorf("resolve backup dir: %w", err)
	}
	parentDir, err := safefs.OpenDirFollow(filepath.Dir(root))
	if err != nil {
		return fmt.Errorf("open backup parent directory: %w", err)
	}
	defer parentDir.Close()
	rootDir, err := parentDir.OpenDirAt(filepath.Base(root))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("backup %s does not exist in %s", backupID, l.Dir)
		}
		return fmt.Errorf("open backup root: %w", err)
	}
	defer rootDir.Close()
	info, err := rootDir.StatAt(backupID)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("backup %s does not exist in %s", backupID, l.Dir)
		}
		return fmt.Errorf("stat backup dir: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("backup %s is not a directory", backupID)
	}
	// The whole tree is dismantled through descriptor-relative operations:
	// every directory is only descended through an O_NOFOLLOW handle, so a
	// member swapped for a symlink is unlinked, never followed, and nothing
	// outside the pinned root can be reached or deleted (#1219).
	if err := rootDir.RemoveTreeAt(backupID); err != nil {
		return fmt.Errorf("remove backup %s: %w", backupID, err)
	}
	return rootDir.File().Sync()
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
