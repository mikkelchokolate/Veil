package backup

import (
	"fmt"
	"os"
	"path/filepath"
)

const backupManifestName = "manifest.json"

type resolvedRestoreEntry struct {
	OriginalPath string
	BackupPath   string
	Mode         os.FileMode
}

func resolveManifestEntries(backupDir string, manifest Manifest) ([]resolvedRestoreEntry, error) {
	absRoot, err := filepath.Abs(backupDir)
	if err != nil {
		return nil, fmt.Errorf("resolve backup dir: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve backup dir: %w", err)
	}

	seenOrig := make(map[string]struct{}, len(manifest.Entries))
	seenMember := make(map[string]struct{}, len(manifest.Entries))
	entries := make([]resolvedRestoreEntry, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		original, err := sanitizeRestoreOriginalPath(entry.OriginalPath, absRoot, resolvedRoot)
		if err != nil {
			return nil, err
		}
		if _, dup := seenOrig[original]; dup {
			return nil, fmt.Errorf("duplicate restore destination %s", original)
		}
		seenOrig[original] = struct{}{}

		member, info, err := resolveBackupMember(absRoot, resolvedRoot, entry.BackupPath)
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

func sanitizeRestoreOriginalPath(path, absRoot, resolvedRoot string) (string, error) {
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
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat original path %s: %w", absOrig, err)
	}
	return absOrig, nil
}

func resolveBackupMember(absRoot, resolvedRoot, backupPath string) (string, os.FileInfo, error) {
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
	info, err := os.Lstat(candidate)
	if err != nil {
		return "", nil, fmt.Errorf("stat backup file %s: %w", candidate, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("backup member is not a regular file: %s", candidate)
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", nil, fmt.Errorf("stat backup file %s: %w", candidate, err)
	}
	if !backupPathWithin(resolvedRoot, resolved) {
		return "", nil, fmt.Errorf("backup member %q is outside the backup directory", backupPath)
	}
	return candidate, info, nil
}
