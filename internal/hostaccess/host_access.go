package hostaccess

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"time"
)

// testHooks are package-level indirections that allow tests to inject errors
// for OS and filesystem operations that are otherwise impossible to trigger
// deterministically (e.g. chmod/chown failing on a directory the test process
// owns). They are initialized to the real implementations and are only
// reassigned by tests.
var testHooks = struct {
	prepareAccountDeps func() AccountDependencies
	lstat              func(string) (os.FileInfo, error)
	chmod              func(string, os.FileMode) error
	chown              func(string, int, int) error
	walkDir            func(string, fs.WalkDirFunc) error
	copy               func(io.Writer, io.Reader) (int64, error)
}{
	prepareAccountDeps: DefaultAccountDependencies,
	lstat:              os.Lstat,
	chmod:              os.Chmod,
	chown:              os.Chown,
	walkDir:            filepath.WalkDir,
	copy:               io.Copy,
}

type Identity struct {
	UID      int
	GID      int
	ProxyUID int
	ProxyGID int
}

type Paths struct {
	EtcDir  string
	VarDir  string
	RootUID int
	RootGID int
}

type AccountDependencies struct {
	LookupUser  func(string) (*user.User, error)
	LookupGroup func(string) (*user.Group, error)
	Run         func(string, ...string) error
}

func DefaultAccountDependencies() AccountDependencies {
	return AccountDependencies{
		LookupUser:  user.Lookup,
		LookupGroup: user.LookupGroup,
		Run: func(name string, args ...string) error {
			return exec.Command(name, args...).Run()
		},
	}
}

func Prepare(paths Paths) error {
	identity, err := EnsureAccount(testHooks.prepareAccountDeps())
	if err != nil {
		return err
	}
	return Migrate(paths, identity, time.Now)
}

func EnsureAccount(deps AccountDependencies) (Identity, error) {
	if deps.LookupUser == nil || deps.LookupGroup == nil || deps.Run == nil {
		return Identity{}, fmt.Errorf("host account dependencies are incomplete")
	}
	panel, err := ensureNamedAccount(deps, "veil")
	if err != nil {
		return Identity{}, err
	}
	proxy, err := ensureNamedAccount(deps, "veil-proxy")
	if err != nil {
		return Identity{}, err
	}
	if err := addSupplementaryGroup(deps, "veil", "veil-proxy"); err != nil {
		return Identity{}, err
	}
	panel.ProxyUID = proxy.UID
	panel.ProxyGID = proxy.GID
	return panel, nil
}

func ensureNamedAccount(deps AccountDependencies, name string) (Identity, error) {
	group, err := deps.LookupGroup(name)
	if err != nil {
		if err := deps.Run("groupadd", "--system", name); err != nil {
			if fallbackErr := deps.Run("addgroup", "-S", name); fallbackErr != nil {
				return Identity{}, fmt.Errorf("create %s group: %v; fallback: %w", name, err, fallbackErr)
			}
		}
		group, err = deps.LookupGroup(name)
		if err != nil {
			return Identity{}, fmt.Errorf("resolve created %s group: %w", name, err)
		}
	}
	account, err := deps.LookupUser(name)
	if err != nil {
		if err := deps.Run(
			"useradd", "--system", "--gid", name, "--no-create-home",
			"--home-dir", "/nonexistent", "--shell", "/usr/sbin/nologin", name,
		); err != nil {
			if fallbackErr := deps.Run(
				"adduser", "-S", "-D", "-H", "-h", "/nonexistent", "-s", "/sbin/nologin", "-G", name, name,
			); fallbackErr != nil {
				return Identity{}, fmt.Errorf("create %s user: %v; fallback: %w", name, err, fallbackErr)
			}
		}
		account, err = deps.LookupUser(name)
		if err != nil {
			return Identity{}, fmt.Errorf("resolve created %s user: %w", name, err)
		}
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return Identity{}, fmt.Errorf("parse %s uid: %w", name, err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return Identity{}, fmt.Errorf("parse %s gid: %w", name, err)
	}
	if account.Gid != group.Gid {
		return Identity{}, fmt.Errorf("%s user primary gid %s does not match %s group gid %s", name, account.Gid, name, group.Gid)
	}
	return Identity{UID: uid, GID: gid}, nil
}

func addSupplementaryGroup(deps AccountDependencies, userName, groupName string) error {
	if err := deps.Run("usermod", "-aG", groupName, userName); err != nil {
		if fallbackErr := deps.Run("addgroup", userName, groupName); fallbackErr != nil {
			return fmt.Errorf("add %s to %s: %v; fallback: %w", userName, groupName, err, fallbackErr)
		}
	}
	return nil
}

func Migrate(paths Paths, panel Identity, now func() time.Time) error {
	if paths.EtcDir == "" || paths.VarDir == "" {
		return fmt.Errorf("etc and var directories are required")
	}
	if now == nil {
		now = time.Now
	}
	if err := ensureOwnedDirectory(paths.EtcDir, 0o751, paths.RootUID, panel.GID); err != nil {
		return err
	}
	if err := ensureOwnedDirectory(paths.VarDir, 0o750, panel.UID, panel.GID); err != nil {
		return err
	}
	safetyRoot, err := createSafetyCopies(paths, now())
	if err != nil {
		return err
	}
	if safetyRoot != "" {
		if err := applyTreeOwnership(safetyRoot, 0o700, 0o600, paths.RootUID, paths.RootGID); err != nil {
			return err
		}
	}

	for _, dir := range []string{"audit", "staging", "updates", "autocert"} {
		if err := applyTreeOwnership(filepath.Join(paths.VarDir, dir), 0o700, 0o600, panel.UID, panel.GID); err != nil {
			return err
		}
	}
	// The naive fallback site moved to etcDir/www: veil-caddy runs as
	// veil-proxy with the var-dir masked, so a legacy varDir/www tree is
	// unreachable to it. Carry its content into the new root (regular files
	// and directories only — symlinks never cross into a veil-proxy-readable
	// tree) and leave the legacy directory veil-owned for the operator.
	legacyWWW := filepath.Join(paths.VarDir, "www")
	info, err := testHooks.lstat(legacyWWW)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("refuse to migrate non-directory legacy fallback root %s", legacyWWW)
		}
		if err := copyLegacyWWWTree(legacyWWW, filepath.Join(paths.EtcDir, "www")); err != nil {
			return err
		}
		if err := applyTreeOwnership(legacyWWW, 0o750, 0o640, panel.UID, panel.GID); err != nil {
			return err
		}
	case !os.IsNotExist(err):
		return err
	}
	for _, name := range []string{"state.json", "sessions.json"} {
		if err := setOptionalFile(filepath.Join(paths.VarDir, name), 0o600, panel.UID, panel.GID); err != nil {
			return err
		}
	}
	for _, dir := range []string{"backups", "promotion-backups", "migration-backups"} {
		if err := applyBackupTreeOwnership(filepath.Join(paths.VarDir, dir), paths.RootUID, paths.RootGID); err != nil {
			return err
		}
	}

	generatedGID := panel.GID
	if panel.ProxyGID != 0 {
		generatedGID = panel.ProxyGID
	}
	// generated/, tls/, certs/ and www/ hold material the veil-proxy units
	// read (rendered configs, synced ACME pairs, the naive fallback site):
	// root-owned, veil-proxy group.
	for _, dir := range []string{"generated", "tls", "certs", "www"} {
		if err := applyTreeOwnership(filepath.Join(paths.EtcDir, dir), 0o750, 0o640, paths.RootUID, generatedGID); err != nil {
			return err
		}
	}
	// The panel's TLS material (local/direct access) lives here and is read by
	// the veil-owned Panel process and by the protocol units (User=veil-proxy)
	// that share the panel certificate. Group it veil-proxy like generated/ and
	// tls/ so both readers can open it (audit #354).
	if err := applyTreeOwnership(filepath.Join(paths.EtcDir, "panel"), 0o750, 0o640, paths.RootUID, generatedGID); err != nil {
		return err
	}
	for _, name := range []string{"state.key", "veil.env"} {
		if err := setOptionalFile(filepath.Join(paths.EtcDir, name), 0o640, paths.RootUID, panel.GID); err != nil {
			return err
		}
	}
	return setOptionalFile(filepath.Join(paths.EtcDir, "backup.passphrase"), 0o600, paths.RootUID, paths.RootGID)
}

func createSafetyCopies(paths Paths, now time.Time) (string, error) {
	type source struct {
		path string
		name string
	}
	sources := []source{
		{filepath.Join(paths.EtcDir, "state.key"), "state.key"},
		{filepath.Join(paths.EtcDir, "veil.env"), "veil.env"},
		{filepath.Join(paths.VarDir, "state.json"), "state.json"},
		{filepath.Join(paths.VarDir, "sessions.json"), "sessions.json"},
	}
	existing := make([]source, 0, len(sources))
	for _, source := range sources {
		info, err := testHooks.lstat(source.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("refuse to migrate non-regular managed file %s", source.path)
		}
		existing = append(existing, source)
	}
	if len(existing) == 0 {
		return "", nil
	}
	base := filepath.Join(paths.VarDir, "migration-backups")
	if err := ensureOwnedDirectory(base, 0o700, paths.RootUID, paths.RootGID); err != nil {
		return "", err
	}
	root := filepath.Join(base, now.UTC().Format("20060102T150405Z"))
	for suffix := 0; ; suffix++ {
		candidate := root
		if suffix > 0 {
			candidate = fmt.Sprintf("%s-%d", root, suffix)
		}
		err := os.Mkdir(candidate, 0o700)
		if err == nil {
			root = candidate
			break
		}
		if !os.IsExist(err) {
			return "", err
		}
	}
	for _, source := range existing {
		if err := copyRegularFile(source.path, filepath.Join(root, source.name)); err != nil {
			return "", err
		}
	}
	return root, nil
}

// copyLegacyWWWTree copies regular files and directories from the legacy
// fallback root into the new one, never overwriting existing destination
// entries and never following or copying symlinks.
func copyLegacyWWWTree(src, dst string) error {
	return testHooks.walkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// DirEntry never follows links, so a symlink reports IsDir()==false;
		// skip it outright rather than copying a link into a
		// veil-proxy-readable tree.
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if _, err := testHooks.lstat(target); err == nil {
			// Existing destination content wins over legacy content. A
			// directory must still be descended so nested legacy files land.
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		return copyRegularFile(path, target)
	})
}

func copyRegularFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := testHooks.copy(output, input); err != nil {
		return errors.Join(err, output.Close())
	}
	return output.Close()
}

func ensureOwnedDirectory(path string, mode os.FileMode, uid, gid int) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	if err := testHooks.chmod(path, mode); err != nil {
		return err
	}
	return testHooks.chown(path, uid, gid)
}

func applyBackupTreeOwnership(root string, uid, gid int) error {
	if err := ensureOwnedDirectory(root, 0o700, uid, gid); err != nil {
		return err
	}
	return testHooks.walkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse to migrate symlink %s", path)
		}
		if entry.IsDir() {
			if err := testHooks.chmod(path, 0o700); err != nil {
				return err
			}
		}
		return testHooks.chown(path, uid, gid)
	})
}

func applyTreeOwnership(root string, dirMode, fileMode os.FileMode, uid, gid int) error {
	if err := ensureOwnedDirectory(root, dirMode, uid, gid); err != nil {
		return err
	}
	return testHooks.walkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse to migrate symlink %s", path)
		}
		mode := fileMode
		if entry.IsDir() {
			mode = dirMode
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse to migrate non-regular path %s", path)
		}
		if err := testHooks.chmod(path, mode); err != nil {
			return err
		}
		return testHooks.chown(path, uid, gid)
	})
}

func setOptionalFile(path string, mode os.FileMode, uid, gid int) error {
	info, err := testHooks.lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("refuse to migrate non-regular managed file %s", path)
	}
	if err := testHooks.chmod(path, mode); err != nil {
		return err
	}
	return testHooks.chown(path, uid, gid)
}
