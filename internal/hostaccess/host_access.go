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
	// MitaUID/MitaGID identify the dedicated veil-mita runtime account that
	// owns veil-mieru.service's appctl socket and state dir (issue #624).
	// Zero means the caller has no mita identity — callers that did not run
	// EnsureAccount leave it unset and Migrate skips the mita state dir.
	MitaUID int
	MitaGID int
}

type Paths struct {
	EtcDir  string
	VarDir  string
	RootUID int
	RootGID int
	// MitaDir is the mieru daemon StateDirectory. Empty resolves to the
	// fixed systemd location /var/lib/mita — StateDirectory=mita always
	// resolves under /var/lib regardless of the configured VarDir
	// (issue #660).
	MitaDir string
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
	// veil-mita is the dedicated mieru daemon identity: the appctl UDS is a
	// control plane, so it must NOT share the veil-proxy edge uid/group —
	// any compromised veil-proxy unit could otherwise drive it (issue #624).
	mita, err := ensureNamedAccount(deps, "veil-mita")
	if err != nil {
		return Identity{}, err
	}
	if err := addSupplementaryGroup(deps, "veil", "veil-proxy"); err != nil {
		return Identity{}, err
	}
	// The panel connects to the appctl socket through the veil-mita group;
	// veil-proxy units are deliberately NOT members.
	if err := addSupplementaryGroup(deps, "veil", "veil-mita"); err != nil {
		return Identity{}, err
	}
	panel.ProxyUID = proxy.UID
	panel.ProxyGID = proxy.GID
	panel.MitaUID = mita.UID
	panel.MitaGID = mita.GID
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
	// veil-caddy.service moved to User=veil-proxy (#497/#615). systemd never
	// re-owns an existing StateDirectory, so a /var/lib/caddy left behind by
	// the previous identity stays unwritable to the new unit — mirror the
	// package postinstall and re-own existing real directories to the proxy
	// identity (issue #623). /var/lib/mita is deliberately NOT in this set:
	// veil-mieru.service now runs as the dedicated veil-mita identity
	// (#624), so the mita tree gets its own pass below (issue #660).
	// StateDirectory names always resolve under /var/lib regardless of the
	// configured VarDir, hence the fixed paths.
	if panel.ProxyUID != 0 || panel.ProxyGID != 0 {
		for _, dir := range proxyStateDirs {
			if err := reownProxyStateDir(dir, panel.ProxyUID, panel.ProxyGID); err != nil {
				return err
			}
		}
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
	if err := setOptionalFile(filepath.Join(paths.EtcDir, "backup.passphrase"), 0o600, paths.RootUID, paths.RootGID); err != nil {
		return err
	}
	// veil-mieru.service moved to the dedicated veil-mita identity (issue
	// #624). systemd does not re-own an existing StateDirectory, so a mita
	// state tree left at veil-proxy:veil-proxy would be unwritable for the
	// daemon — re-own it like the packaged postinstall does.
	if panel.MitaUID != 0 && panel.MitaGID != 0 {
		mitaDir := paths.MitaDir
		if mitaDir == "" {
			// systemd StateDirectory=mita is fixed at /var/lib/mita — it
			// does NOT follow a custom --var-dir. Defaulting to a VarDir
			// sibling would leave the real daemon state dir untouched
			// (issue #660).
			mitaDir = defaultMitaStateDir
		}
		info, err := testHooks.lstat(mitaDir)
		switch {
		case os.IsNotExist(err):
			// No daemon state yet — systemd creates it veil-mita-owned.
		case err != nil:
			return err
		case info.Mode()&os.ModeSymlink != 0 || !info.IsDir():
			return fmt.Errorf("refuse to migrate non-directory mita state dir %s", mitaDir)
		default:
			if err := applyTreeOwnership(mitaDir, 0o700, 0o600, panel.MitaUID, panel.MitaGID); err != nil {
				return err
			}
		}
	}
	return nil
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

// proxyStateDirs are the fixed StateDirectory trees that must belong to the
// veil-proxy identity (veil-caddy.service StateDirectory=caddy).
// /var/lib/mita is NOT here: it belongs to the dedicated veil-mita identity
// since #624, and the mita pass below owns it (issue #660). It is a
// variable so tests can point it at a scratch tree.
var proxyStateDirs = []string{"/var/lib/caddy"}

// defaultMitaStateDir is the fixed systemd StateDirectory=mita location.
// systemd always resolves it under /var/lib no matter what --var-dir the
// install used, so the default never derives from Paths.VarDir. It is a
// variable so tests can point it at a scratch tree.
var defaultMitaStateDir = "/var/lib/mita"

// reownProxyStateDir mirrors the package postinstall `chown -R
// veil-proxy:veil-proxy` repair: an existing real directory tree is re-owned
// to the proxy identity, contents included. A symlinked or non-directory
// path is operator-managed and left alone, and symlinks inside the tree are
// never followed — matching `chown -R` semantics.
func reownProxyStateDir(root string, uid, gid int) error {
	info, err := testHooks.lstat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil
	}
	return testHooks.walkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			// Never chown through a link: only the tree's own entries are
			// re-owned, like `chown -R` without -L.
			return nil
		}
		return testHooks.chown(path, uid, gid)
	})
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
