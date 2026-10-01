package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

const backupManifestName = "manifest.json"

type resolvedRestoreEntry struct {
	OriginalPath string
	// BackupPath is the member path RELATIVE to the pinned backup directory,
	// slash-separated. It is only ever resolved descriptor-relative, never
	// against a mutable path (#1219).
	BackupPath string
	Mode       os.FileMode
}

func resolveManifestEntries(backupDir *safefs.Dir, absRoot string, manifest Manifest, allowedRoots []string) ([]resolvedRestoreEntry, error) {
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve backup dir: %w", err)
	}

	seenOrig := make(map[string]struct{}, len(manifest.Entries))
	seenMember := make(map[string]struct{}, len(manifest.Entries))
	entries := make([]resolvedRestoreEntry, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		original, err := sanitizeRestoreOriginalPath(entry.OriginalPath, absRoot, resolvedRoot, allowedRoots)
		if err != nil {
			return nil, err
		}
		if _, dup := seenOrig[original]; dup {
			return nil, fmt.Errorf("duplicate restore destination %s", original)
		}
		seenOrig[original] = struct{}{}

		member, info, err := resolveBackupMember(backupDir, absRoot, entry.BackupPath)
		if err != nil {
			return nil, err
		}
		if _, dup := seenMember[member]; dup {
			return nil, fmt.Errorf("duplicate backup member %s", member)
		}
		seenMember[member] = struct{}{}

		entries = append(entries, resolvedRestoreEntry{
			OriginalPath: original,
			BackupPath:   member,
			Mode:         info.Mode(),
		})
	}
	return entries, nil
}

func sanitizeRestoreOriginalPath(path, absRoot, resolvedRoot string, allowedRoots []string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("manifest original path is empty")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("manifest original path is not absolute: %s", path)
	}
	cleaned := filepath.Clean(path)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("manifest original path is not absolute: %s", path)
	}
	absOrig, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve original path: %w", err)
	}
	if absOrig == absRoot || absOrig == resolvedRoot || backupPathWithin(absRoot, absOrig) || backupPathWithin(resolvedRoot, absOrig) {
		return "", fmt.Errorf("manifest original path %s is inside the backup directory", absOrig)
	}
	// Managed-root allowlist: when configured, a manifest-declared
	// destination must live under one of the allowed roots — lexically, and
	// after resolving symlinks for an existing leaf (#1219).
	if len(allowedRoots) > 0 && !backupPathUnderAnyRoot(allowedRoots, absOrig) {
		return "", fmt.Errorf("restore destination %s is outside the allowed roots", absOrig)
	}
	info, err := os.Lstat(absOrig)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("restore destination is not a regular file: %s", absOrig)
		}
		resolvedOrig, err := filepath.EvalSymlinks(absOrig)
		if err != nil {
			return "", fmt.Errorf("resolve original path: %w", err)
		}
		if resolvedOrig == resolvedRoot || backupPathWithin(resolvedRoot, resolvedOrig) {
			return "", fmt.Errorf("manifest original path %s is inside the backup directory", absOrig)
		}
		if len(allowedRoots) > 0 && !backupPathUnderAnyRoot(allowedRoots, resolvedOrig) {
			return "", fmt.Errorf("restore destination %s is outside the allowed roots", absOrig)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat original path %s: %w", absOrig, err)
	}
	return absOrig, nil
}

func backupPathUnderAnyRoot(roots []string, path string) bool {
	for _, root := range roots {
		if path == root || backupPathWithin(root, path) {
			return true
		}
	}
	return false
}

// resolveBackupMember validates the manifest-declared member path and checks
// it through the pinned backup directory: intermediate components are only
// descended through descriptor-relative O_NOFOLLOW opens, so a directory or
// leaf swapped for a symlink is rejected rather than followed (#1219).
// Returns the slash-separated member path relative to the backup directory.
func resolveBackupMember(backupDir *safefs.Dir, absRoot, backupPath string) (string, os.FileInfo, error) {
	if backupPath == "" {
		return "", nil, fmt.Errorf("manifest backup path is empty")
	}
	var candidate string
	if filepath.IsAbs(backupPath) {
		candidate = filepath.Clean(backupPath)
	} else {
		if !filepath.IsLocal(backupPath) {
			return "", nil, fmt.Errorf("backup member %q is outside the backup directory", backupPath)
		}
		candidate = filepath.Join(absRoot, filepath.Clean(backupPath))
	}
	if filepath.Base(candidate) == backupManifestName {
		return "", nil, fmt.Errorf("backup member must not be %s", backupManifestName)
	}
	if !backupPathWithin(absRoot, candidate) {
		return "", nil, fmt.Errorf("backup member %q is outside the backup directory", backupPath)
	}
	rel, err := filepath.Rel(absRoot, candidate)
	if err != nil || rel == "" || !filepath.IsLocal(rel) {
		return "", nil, fmt.Errorf("backup member %q is outside the backup directory", backupPath)
	}
	// Pinned validation walk: every intermediate component must be a real
	// directory (OpenDirAt rejects a swapped symlink), and the leaf must be
	// a regular file with a single link.
	dir := backupDir
	opened := []*safefs.Dir{}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts[:len(parts)-1] {
		next, err := dir.OpenDirAt(part)
		if err != nil {
			for _, openedDir := range opened {
				_ = openedDir.Close()
			}
			return "", nil, fmt.Errorf("stat backup file %s: %w", candidate, err)
		}
		opened = append(opened, next)
		dir = next
	}
	info, err := dir.StatAt(parts[len(parts)-1])
	for _, openedDir := range opened {
		_ = openedDir.Close()
	}
	if err != nil {
		return "", nil, fmt.Errorf("stat backup file %s: %w", candidate, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("backup member is not a regular file: %s", candidate)
	}
	if unsafeJournalHardLink(info) {
		return "", nil, fmt.Errorf("backup member has unsafe hard links: %s", candidate)
	}
	return filepath.ToSlash(rel), info, nil
}

// openBackupMemberAt opens a validated member (slash-separated relative
// path) for reading through the pinned backup directory — the same
// descriptor-relative walk used at validation time, so a member swapped
// between validation and read is refused, not followed (#1219).
func openBackupMemberAt(backupDir *safefs.Dir, rel string) (*os.File, error) {
	parts := strings.Split(rel, "/")
	dir := backupDir
	opened := []*safefs.Dir{}
	for _, part := range parts[:len(parts)-1] {
		next, err := dir.OpenDirAt(part)
		if err != nil {
			for _, openedDir := range opened {
				_ = openedDir.Close()
			}
			return nil, fmt.Errorf("open backup member: %w", err)
		}
		opened = append(opened, next)
		dir = next
	}
	file, err := dir.OpenFileAt(parts[len(parts)-1])
	for _, openedDir := range opened {
		_ = openedDir.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("open backup member: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("backup member is not a regular file: %s", rel)
	}
	return file, nil
}
