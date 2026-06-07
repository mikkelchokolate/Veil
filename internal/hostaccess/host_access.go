package hostaccess

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"time"
)

type Identity struct {
	UID int
	GID int
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
	identity, err := EnsureAccount(DefaultAccountDependencies())
	if err != nil {
		return err
	}
	return Migrate(paths, identity, time.Now)
}

func EnsureAccount(deps AccountDependencies) (Identity, error) {
	if deps.LookupUser == nil || deps.LookupGroup == nil || deps.Run == nil {
		return Identity{}, fmt.Errorf("host account dependencies are incomplete")
	}
	group, err := deps.LookupGroup("veil")
	if err != nil {
		if err := deps.Run("groupadd", "--system", "veil"); err != nil {
			if fallbackErr := deps.Run("addgroup", "-S", "veil"); fallbackErr != nil {
				return Identity{}, fmt.Errorf("create veil group: %v; fallback: %w", err, fallbackErr)
			}
		}
		group, err = deps.LookupGroup("veil")
		if err != nil {
			return Identity{}, fmt.Errorf("resolve created veil group: %w", err)
		}
	}
	account, err := deps.LookupUser("veil")
	if err != nil {
		if err := deps.Run(
			"useradd", "--system", "--gid", "veil", "--no-create-home",
			"--home-dir", "/nonexistent", "--shell", "/usr/sbin/nologin", "veil",
		); err != nil {
			if fallbackErr := deps.Run(
				"adduser", "-S", "-D", "-H", "-h", "/nonexistent", "-s", "/sbin/nologin", "-G", "veil", "veil",
			); fallbackErr != nil {
				return Identity{}, fmt.Errorf("create veil user: %v; fallback: %w", err, fallbackErr)
			}
		}
		account, err = deps.LookupUser("veil")
		if err != nil {
			return Identity{}, fmt.Errorf("resolve created veil user: %w", err)
		}
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return Identity{}, fmt.Errorf("parse veil uid: %w", err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return Identity{}, fmt.Errorf("parse veil gid: %w", err)
	}
	if account.Gid != group.Gid {
		return Identity{}, fmt.Errorf("veil user primary gid %s does not match veil group gid %s", account.Gid, group.Gid)
	}
	return Identity{UID: uid, GID: gid}, nil
}

func Migrate(paths Paths, panel Identity, now func() time.Time) error {
	if paths.EtcDir == "" || paths.VarDir == "" {
		return fmt.Errorf("etc and var directories are required")
	}
	if now == nil {
		now = time.Now
	}
	if err := ensureOwnedDirectory(paths.EtcDir, 0o750, paths.RootUID, panel.GID); err != nil {
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
	if err := applyTreeOwnership(filepath.Join(paths.VarDir, "www"), 0o750, 0o640, panel.UID, panel.GID); err != nil {
		return err
	}
	for _, name := range []string{"state.json", "sessions.json"} {
		if err := setOptionalFile(filepath.Join(paths.VarDir, name), 0o600, panel.UID, panel.GID); err != nil {
			return err
		}
	}
	for _, dir := range []string{"backups", "promotion-backups", "migration-backups"} {
		if err := applyTreeOwnership(filepath.Join(paths.VarDir, dir), 0o700, 0o600, paths.RootUID, paths.RootGID); err != nil {
			return err
		}
	}

	if err := applyTreeOwnership(filepath.Join(paths.EtcDir, "generated"), 0o750, 0o640, paths.RootUID, panel.GID); err != nil {
		return err
	}
	if err := applyTreeOwnership(filepath.Join(paths.EtcDir, "tls"), 0o750, 0o640, paths.RootUID, panel.GID); err != nil {
		return err
	}
	// The panel's self-signed TLS material (local/direct access) lives here and is
	// read by the veil-owned Panel process, so it must be group-readable by veil.
	if err := applyTreeOwnership(filepath.Join(paths.EtcDir, "panel"), 0o750, 0o640, paths.RootUID, panel.GID); err != nil {
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
		info, err := os.Lstat(source.path)
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
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	return output.Close()
}

func ensureOwnedDirectory(path string, mode os.FileMode, uid, gid int) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	return os.Chown(path, uid, gid)
}

func applyTreeOwnership(root string, dirMode, fileMode os.FileMode, uid, gid int) error {
	if err := ensureOwnedDirectory(root, dirMode, uid, gid); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
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
		if err := os.Chmod(path, mode); err != nil {
			return err
		}
		return os.Chown(path, uid, gid)
	})
}

func setOptionalFile(path string, mode os.FileMode, uid, gid int) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("refuse to migrate non-regular managed file %s", path)
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	return os.Chown(path, uid, gid)
}
