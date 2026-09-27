package hostaccess

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/mikkelchokolate/Veil/internal/safefs"
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
	copy               func(io.Writer, io.Reader) (int64, error)
	// Tree walks are descriptor-pinned (#1127): every per-entry mutation is
	// resolved relative to the held parent descriptor, never by re-resolving
	// a multi-component path the service accounts can influence. The walk
	// driver itself cannot live in this struct — its default calls back into
	// testHooks, which the compiler would flag as an initialization cycle —
	// so tests override walkManagedDirHook instead.
	statEntryAt    func(dir *safefs.Dir, name string) (os.FileInfo, error)
	chmodDir       func(dir *safefs.Dir, mode os.FileMode) error
	chownDir       func(dir *safefs.Dir, uid, gid int) error
	chmodEntryAt   func(e *managedEntry, mode os.FileMode) error
	chownEntryAt   func(e *managedEntry, uid, gid int) error
	openEntryAt    func(e *managedEntry) (*os.File, error)
	mkdirEntryAt   func(dir *safefs.Dir, name string, mode os.FileMode) error
	openDirEntryAt func(dir *safefs.Dir, name string) (*safefs.Dir, error)
}{
	prepareAccountDeps: DefaultAccountDependencies,
	lstat:              os.Lstat,
	chmod:              safefs.ChmodNoFollow,
	chown:              safefs.ChownNoFollow,
	copy:               io.Copy,
	statEntryAt:        func(dir *safefs.Dir, name string) (os.FileInfo, error) { return dir.StatAt(name) },
	chmodDir:           func(dir *safefs.Dir, mode os.FileMode) error { return dir.File().Chmod(mode) },
	chownDir:           func(dir *safefs.Dir, uid, gid int) error { return dir.File().Chown(uid, gid) },
	chmodEntryAt:       func(e *managedEntry, mode os.FileMode) error { return e.parent.ChmodAt(e.name, mode) },
	chownEntryAt:       func(e *managedEntry, uid, gid int) error { return e.parent.ChownAt(e.name, uid, gid) },
	openEntryAt:        func(e *managedEntry) (*os.File, error) { return e.parent.OpenFileAt(e.name) },
	mkdirEntryAt:       func(dir *safefs.Dir, name string, mode os.FileMode) error { return dir.MkdirAt(name, mode) },
	openDirEntryAt:     func(dir *safefs.Dir, name string) (*safefs.Dir, error) { return dir.OpenDirAt(name) },
}

// walkManagedDirHook is the test-only override for walkManagedDir; nil means
// use the real descriptor-pinned implementation.
var walkManagedDirHook func(dir *safefs.Dir, visit func(*managedEntry) error) error

func walkManagedDir(dir *safefs.Dir, visit func(*managedEntry) error) error {
	if walkManagedDirHook != nil {
		return walkManagedDirHook(dir, visit)
	}
	return walkManagedDirEntries(dir, visit)
}

// managedEntry is one non-root entry discovered by walkManagedDirEntries.
// Mutations are pinned to the parent directory descriptor the walk holds
// open, so a symlink swapped into any ancestor component — or the leaf —
// cannot redirect chmod/chown/open outside the managed tree (#1127). Path()
// is a display path for errors; the operations never resolve it.
type managedEntry struct {
	parent *safefs.Dir
	name   string
	info   os.FileInfo
}

// Path returns the display path for error messages.
func (e *managedEntry) Path() string { return filepath.Join(e.parent.Path(), e.name) }

// Info returns the descriptor-pinned lstat-equivalent metadata.
func (e *managedEntry) Info() os.FileInfo { return e.info }

// IsDir reports whether the pinned stat saw a directory.
func (e *managedEntry) IsDir() bool { return e.info.IsDir() }

func (e *managedEntry) chmod(mode os.FileMode) error { return testHooks.chmodEntryAt(e, mode) }
func (e *managedEntry) chown(uid, gid int) error     { return testHooks.chownEntryAt(e, uid, gid) }
func (e *managedEntry) open() (*os.File, error)      { return testHooks.openEntryAt(e) }

// walkManagedDirEntries enumerates dir in lexical order and calls visit for
// each entry, recursing into real directories. Every entry is statted
// relative to the held descriptor (Fstatat + AT_SYMLINK_NOFOLLOW), and
// descent opens children relative to it too — a directory swapped for a
// symlink between the stat and the descent fails ELOOP instead of escaping
// the tree (#1127).
func walkManagedDirEntries(dir *safefs.Dir, visit func(*managedEntry) error) error {
	dirents, err := dir.ReadDir()
	if err != nil {
		return err
	}
	sort.Slice(dirents, func(i, j int) bool { return dirents[i].Name() < dirents[j].Name() })
	for _, de := range dirents {
		name := de.Name()
		if name == "." || name == ".." {
			continue
		}
		info, err := testHooks.statEntryAt(dir, name)
		if err != nil {
			return err
		}
		e := &managedEntry{parent: dir, name: name, info: info}
		if err := visit(e); err != nil {
			return err
		}
		if info.IsDir() {
			sub, err := testHooks.openDirEntryAt(dir, name)
			if err != nil {
				return err
			}
			err = walkManagedDir(sub, visit)
			closeErr := sub.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	return nil
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
	safetyDir, err := createSafetyCopies(paths, now())
	if err != nil {
		return err
	}
	if safetyDir != nil {
		// The safety tree is owned on the pinned descriptor, never by
		// re-resolving its path: migration-backups sits inside the
		// service-owned VarDir and could be swapped for a symlink between
		// createSafetyCopies and a path-based pass (#1127).
		err := applyManagedDirOwnership(safetyDir, 0o700, 0o600, paths.RootUID, paths.RootGID)
		closeErr := safetyDir.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
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

// createSafetyCopies snapshots the managed secret files into a fresh
// timestamped directory under VarDir/migration-backups. It returns the
// timestamped root as an open *safefs.Dir (nil when there is nothing to
// copy) so the caller can apply ownership without re-resolving the path
// (#1127).
func createSafetyCopies(paths Paths, now time.Time) (*safefs.Dir, error) {
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
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("refuse to migrate non-regular managed file %s", source.path)
		}
		existing = append(existing, source)
	}
	if len(existing) == 0 {
		return nil, nil
	}
	base := filepath.Join(paths.VarDir, "migration-backups")
	if err := ensureOwnedDirectory(base, 0o700, paths.RootUID, paths.RootGID); err != nil {
		return nil, err
	}
	// migration-backups is a direct child of the service-owned VarDir, so a
	// compromised service identity can rename it or swap it for a symlink at
	// any point after the ownership pass above. Pin the directory descriptor
	// and create the timestamped root and member copies fd-relative — no
	// path under the service-influenced tree is ever re-resolved (#1127).
	baseDir, err := safefs.OpenDir(base)
	if err != nil {
		return nil, err
	}
	defer baseDir.Close()
	stamp := now.UTC().Format("20060102T150405Z")
	rootName := stamp
	for suffix := 0; ; suffix++ {
		candidate := stamp
		if suffix > 0 {
			candidate = fmt.Sprintf("%s-%d", stamp, suffix)
		}
		err := testHooks.mkdirEntryAt(baseDir, candidate, 0o700)
		if err == nil {
			rootName = candidate
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	rootDir, err := testHooks.openDirEntryAt(baseDir, rootName)
	if err != nil {
		return nil, err
	}
	for _, source := range existing {
		if err := copyRegularFileAt(rootDir, source.path, source.name); err != nil {
			_ = rootDir.Close()
			return nil, err
		}
	}
	return rootDir, nil
}

// copyLegacyWWWTree copies regular files and directories from the legacy
// fallback root into the new one, never overwriting existing destination
// entries and never following or copying symlinks. The source tree is
// service-account-owned, so the walk and every source open are pinned to
// held directory descriptors: an ancestor directory or leaf swapped for a
// symlink cannot redirect reads outside the legacy root (#1127).
func copyLegacyWWWTree(src, dst string) error {
	dir, err := safefs.OpenDir(src)
	if err != nil {
		return err
	}
	defer dir.Close()
	return walkManagedDir(dir, func(e *managedEntry) error {
		info := e.Info()
		if info.Mode()&os.ModeSymlink != 0 {
			// Never copy a link into a veil-proxy-readable tree.
			return nil
		}
		rel, err := filepath.Rel(src, e.Path())
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
		if e.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		input, err := e.open()
		if err != nil {
			return err
		}
		// A regular file swapped for a FIFO between the walk stat and this
		// open must not be copied (O_NONBLOCK keeps the open itself from
		// hanging — #1083).
		openInfo, err := input.Stat()
		if err != nil {
			return errors.Join(err, input.Close())
		}
		if !openInfo.Mode().IsRegular() {
			return input.Close()
		}
		err = copyOpenedRegularFile(input, target)
		return errors.Join(err, input.Close())
	})
}

// copyRegularFileAt copies the leaf file srcPath into dstDir as name. The
// destination write is descriptor-relative so no component under the
// service-influenced tree is re-resolved (#1127).
func copyRegularFileAt(dstDir *safefs.Dir, srcPath, name string) error {
	input, err := safefs.OpenNoFollow(srcPath)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refuse to migrate non-regular managed file %s", srcPath)
	}
	output, err := dstDir.CreateFileAt(name, 0o600)
	if err != nil {
		return err
	}
	if _, err := testHooks.copy(output, input); err != nil {
		return errors.Join(err, output.Close())
	}
	return output.Close()
}

func copyRegularFile(source, destination string) error {
	// O_NOFOLLOW|O_NONBLOCK: the lstat regular-file check and this open are
	// separated by time; a swapped symlink must be rejected, not followed
	// (#1009), and a swapped FIFO must not block the copy (#1083).
	input, err := safefs.OpenNoFollow(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refuse to migrate non-regular managed file %s", source)
	}
	return copyOpenedRegularFile(input, destination)
}

func copyOpenedRegularFile(input *os.File, destination string) error {
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
	if err := testHooks.chown(root, uid, gid); err != nil {
		return err
	}
	dir, err := safefs.OpenDir(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return walkManagedDir(dir, func(e *managedEntry) error {
		if e.Info().Mode()&os.ModeSymlink != 0 {
			// Never chown through a link: only the tree's own entries are
			// re-owned, like `chown -R` without -L.
			return nil
		}
		// Fchownat(AT_SYMLINK_NOFOLLOW) chowns the entry without opening it,
		// so FIFOs and sockets are re-owned exactly like `chown -R` instead
		// of blocking on open(O_RDONLY) or failing ENXIO (#1083).
		return e.chown(uid, gid)
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
	dir, err := safefs.OpenDir(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return walkManagedDir(dir, func(e *managedEntry) error {
		info := e.Info()
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse to migrate symlink %s", e.Path())
		}
		if e.IsDir() {
			if err := e.chmod(0o700); err != nil {
				return err
			}
		}
		// Backup members keep their own permission bits (restore reads them
		// back), so files are re-owned only — never chmodded.
		return e.chown(uid, gid)
	})
}

func applyTreeOwnership(root string, dirMode, fileMode os.FileMode, uid, gid int) error {
	if err := ensureOwnedDirectory(root, dirMode, uid, gid); err != nil {
		return err
	}
	dir, err := safefs.OpenDir(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	return applyManagedTreeEntries(dir, dirMode, fileMode, uid, gid)
}

// applyManagedDirOwnership applies the ownership contract to an already
// pinned directory handle and its tree — used where the root itself must not
// be re-resolved by path (the service accounts can swap ancestors of the
// safety-copy root inside VarDir, #1127).
func applyManagedDirOwnership(dir *safefs.Dir, dirMode, fileMode os.FileMode, uid, gid int) error {
	if err := testHooks.chmodDir(dir, dirMode); err != nil {
		return err
	}
	if err := testHooks.chownDir(dir, uid, gid); err != nil {
		return err
	}
	return applyManagedTreeEntries(dir, dirMode, fileMode, uid, gid)
}

// applyManagedTreeEntries walks dir's children with descriptor-pinned
// stat/chmod/chown operations (#1127).
func applyManagedTreeEntries(dir *safefs.Dir, dirMode, fileMode os.FileMode, uid, gid int) error {
	return walkManagedDir(dir, func(e *managedEntry) error {
		info := e.Info()
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse to migrate symlink %s", e.Path())
		}
		mode := fileMode
		if e.IsDir() {
			mode = dirMode
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse to migrate non-regular path %s", e.Path())
		}
		if err := e.chmod(mode); err != nil {
			return err
		}
		return e.chown(uid, gid)
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
