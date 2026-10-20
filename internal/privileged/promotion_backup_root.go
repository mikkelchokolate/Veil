package privileged

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

// promotionRootOwnerUID is the uid the promotion backup root must belong to:
// the identity of the helper process that creates it. It is deliberately the
// real euid rather than the effectiveUID ownership hook, which tests fake to
// exercise the runtime-artifact chown contract.
var promotionRootOwnerUID = os.Geteuid

// openHelperOwnedDir pins path on a directory descriptor and proves it is a
// directory owned by ownerUID with mode exactly 0700. Transaction roots live
// under the service-writable state tree, so the service account can rename
// the real root away and plant its own directory (symlinks included) in its
// place; only a helper-owned 0700 directory proves the helper authored every
// entry inside it (#1227, #1274).
func openHelperOwnedDir(label, path string, ownerUID uint32) (*safefs.Dir, error) {
	dir, err := safefs.OpenDir(path)
	if err != nil {
		return nil, err
	}
	info, err := dir.File().Stat()
	if err != nil {
		_ = dir.Close()
		return nil, err
	}
	uid, ok := fileOwnerUID(info)
	if !ok {
		_ = dir.Close()
		return nil, fmt.Errorf("%s %s returned unexpected stat type %T", label, path, info.Sys())
	}
	if uid != ownerUID || info.Mode().Perm() != 0o700 {
		_ = dir.Close()
		return nil, fmt.Errorf("%s %s is not a helper-owned 0700 directory (uid=%d mode=%#o)", label, path, uid, info.Mode().Perm())
	}
	return dir, nil
}

// openPromotionBackupRoot creates (if missing) and pins the promotion backup
// root, failing closed unless it is the helper-owned 0700 directory the
// installer/migration (applyBackupTreeOwnership) and the helper itself create.
// A veil-owned replacement would otherwise let the service account author the
// transaction journal and backup manifests — and the symlinks inside the root
// that its SafetyPath/ManifestPath records resolve through — turning recovery
// into an arbitrary root read (exfiltrated into a proxy-readable destination)
// and an arbitrary root delete (#1274).
func openPromotionBackupRoot(root string) (*safefs.Dir, error) {
	if root == "" {
		return nil, errors.New("promotion backup root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create promotion backup root: %w", err)
	}
	dir, err := openHelperOwnedDir("promotion backup root", root, uint32(promotionRootOwnerUID()))
	if err != nil {
		return nil, fmt.Errorf("open promotion backup root: %w", err)
	}
	return dir, nil
}

// promotionBackupMemberParts splits path — a journal/manifest-recorded member
// of the backup tree — into the path components below root. The lexical check
// is only a syntax gate: the components are then resolved one at a time from
// the pinned root descriptor with O_NOFOLLOW, so a symlinked intermediate
// directory cannot redirect the access outside the root (#1274).
func promotionBackupMemberParts(root, path string) ([]string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("promotion backup member %q is not an absolute path", path)
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("promotion backup member %s escapes backup root", path)
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, fmt.Errorf("promotion backup member %s is not canonical", path)
		}
	}
	return parts, nil
}

// openPromotionBackupMemberParent walks every intermediate component of path
// below the pinned root and returns the pinned parent directory plus the leaf
// name. Each step is an O_NOFOLLOW|O_DIRECTORY openat, so a symlink anywhere
// in the chain fails with ELOOP/ENOTDIR instead of being followed.
func openPromotionBackupMemberParent(rootDir *safefs.Dir, root, path string) (*safefs.Dir, string, error) {
	parts, err := promotionBackupMemberParts(root, path)
	if err != nil {
		return nil, "", err
	}
	current, err := rootDir.OpenDirAt(".")
	if err != nil {
		return nil, "", err
	}
	for _, part := range parts[:len(parts)-1] {
		next, err := current.OpenDirAt(part)
		_ = current.Close()
		if err != nil {
			return nil, "", fmt.Errorf("open promotion backup member %s: %w", path, err)
		}
		current = next
	}
	return current, parts[len(parts)-1], nil
}

// readPromotionBackupMember reads a regular file recorded inside the backup
// tree, resolving it descriptor-relative under the pinned root.
func readPromotionBackupMember(rootDir *safefs.Dir, root, path string) (body []byte, resultErr error) {
	parent, leaf, err := openPromotionBackupMemberParent(rootDir, root, path)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	file, err := parent.OpenFileAt(leaf)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("promotion backup member %s is not a regular file", path)
	}
	return io.ReadAll(file)
}

// removePromotionBackupMember unlinks a non-directory member recorded inside
// the backup tree, resolving it descriptor-relative under the pinned root and
// syncing the pinned parent. A missing member is not an error.
func removePromotionBackupMember(rootDir *safefs.Dir, root, path string) (resultErr error) {
	parent, leaf, err := openPromotionBackupMemberParent(rootDir, root, path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, parent.Close()) }()
	if err := parent.RemoveAt(leaf); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return parent.File().Sync()
}

// readPromotionJournalAt reads the transaction journal from the pinned root.
// It reports (nil, nil) when no journal exists.
func readPromotionJournalAt(rootDir *safefs.Dir) (body []byte, resultErr error) {
	file, err := rootDir.OpenFileAt(promotionTransactionJournalName)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("promotion transaction journal is not a regular file")
	}
	return io.ReadAll(file)
}
