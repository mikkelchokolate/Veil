package hostaccess

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEnsureAccountCreatesSystemGroupAndUser(t *testing.T) {
	var commands []string
	groupReady := false
	userReady := false
	deps := AccountDependencies{
		LookupGroup: func(string) (*user.Group, error) {
			if !groupReady {
				return nil, errors.New("missing group")
			}
			return &user.Group{Name: "veil", Gid: "4242"}, nil
		},
		LookupUser: func(string) (*user.User, error) {
			if !userReady {
				return nil, errors.New("missing user")
			}
			return &user.User{Username: "veil", Uid: "4242", Gid: "4242"}, nil
		},
		Run: func(name string, args ...string) error {
			commands = append(commands, name+" "+strings.Join(args, " "))
			if name == "groupadd" {
				groupReady = true
			}
			if name == "useradd" {
				userReady = true
			}
			return nil
		},
	}
	identity, err := EnsureAccount(deps)
	if err != nil {
		t.Fatalf("ensure account: %v", err)
	}
	if identity.UID != 4242 || identity.GID != 4242 {
		t.Fatalf("identity=%+v", identity)
	}
	want := []string{
		"groupadd --system veil",
		"useradd --system --gid veil --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin veil",
	}
	if strings.Join(commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands=%v", commands)
	}
}

func TestMigrateCreatesSafetyCopiesAndScopedPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership and mode test")
	}
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "lib", "veil")
	for _, path := range []string{
		filepath.Join(etcDir, "state.key"),
		filepath.Join(etcDir, "veil.env"),
		filepath.Join(varDir, "state.json"),
		filepath.Join(varDir, "sessions.json"),
		filepath.Join(etcDir, "backup.passphrase"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o666); err != nil {
			t.Fatal(err)
		}
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := strconv.Atoi(current.Uid)
	gid, _ := strconv.Atoi(current.Gid)
	err = Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, func() time.Time {
		return time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertMode(t, filepath.Join(etcDir, "state.key"), 0o640)
	assertMode(t, filepath.Join(etcDir, "veil.env"), 0o640)
	assertMode(t, filepath.Join(etcDir, "backup.passphrase"), 0o600)
	assertMode(t, filepath.Join(varDir, "state.json"), 0o600)
	assertMode(t, filepath.Join(varDir, "sessions.json"), 0o600)
	assertMode(t, filepath.Join(varDir, "backups"), 0o700)
	safetyRoot := filepath.Join(varDir, "migration-backups", "20260605T120000Z")
	for _, name := range []string{"state.key", "veil.env", "state.json", "sessions.json"} {
		if _, err := os.Stat(filepath.Join(safetyRoot, name)); err != nil {
			t.Fatalf("safety copy %s: %v", name, err)
		}
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode=%#o want=%#o", path, got, want)
	}
}
