package hostaccess

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
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

func TestMigrateMakesPanelTLSGroupReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership and mode test")
	}
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "lib", "veil")
	panelDir := filepath.Join(etcDir, "panel")
	if err := os.MkdirAll(panelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Mirror how the installer writes panel TLS: cert readable, key root-only (0600).
	if err := os.WriteFile(filepath.Join(panelDir, "tls.crt"), []byte("cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(panelDir, "tls.key"), []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := strconv.Atoi(current.Uid)
	gid, _ := strconv.Atoi(current.Gid)
	if err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, time.Now); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// The veil-owned Panel process reads its TLS key via the veil group, so the key
	// must be group-readable (0640), not the installer's root-only 0600.
	assertMode(t, panelDir, 0o750)
	assertMode(t, filepath.Join(panelDir, "tls.key"), 0o640)
	assertMode(t, filepath.Join(panelDir, "tls.crt"), 0o640)
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

func TestDefaultAccountDependencies(t *testing.T) {
	deps := DefaultAccountDependencies()
	if deps.LookupUser == nil || deps.LookupGroup == nil || deps.Run == nil {
		t.Fatal("expected non-nil dependencies")
	}
	if err := deps.Run("true"); err != nil {
		t.Fatalf("Run true: %v", err)
	}
}

func TestPrepare(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership and mode test")
	}
	if os.Getuid() != 0 {
		t.Skip("requires root to create veil system user/group")
	}
	root := t.TempDir()
	etcDir := filepath.Join(root, "etc", "veil")
	varDir := filepath.Join(root, "var", "veil")
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := strconv.Atoi(current.Uid)
	gid, _ := strconv.Atoi(current.Gid)
	if err := Prepare(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	assertMode(t, filepath.Join(varDir, "audit"), 0o700)
	assertMode(t, filepath.Join(etcDir, "generated"), 0o750)
}

func TestEnsureAccount(t *testing.T) {
	t.Run("existing group and user", func(t *testing.T) {
		deps := AccountDependencies{
			LookupGroup: func(string) (*user.Group, error) {
				return &user.Group{Name: "veil", Gid: "100"}, nil
			},
			LookupUser: func(string) (*user.User, error) {
				return &user.User{Username: "veil", Uid: "100", Gid: "100"}, nil
			},
			Run: func(string, ...string) error {
				t.Fatal("Run should not be called when group and user exist")
				return nil
			},
		}
		id, err := EnsureAccount(deps)
		if err != nil {
			t.Fatalf("ensure account: %v", err)
		}
		if id.UID != 100 || id.GID != 100 {
			t.Fatalf("identity=%+v", id)
		}
	})

	t.Run("incomplete dependencies", func(t *testing.T) {
		base := AccountDependencies{
			LookupUser:  func(string) (*user.User, error) { return nil, errors.New("x") },
			LookupGroup: func(string) (*user.Group, error) { return nil, errors.New("x") },
			Run:         func(string, ...string) error { return nil },
		}
		for _, tc := range []struct {
			name   string
			mutate func(*AccountDependencies)
		}{
			{"nil LookupUser", func(d *AccountDependencies) { d.LookupUser = nil }},
			{"nil LookupGroup", func(d *AccountDependencies) { d.LookupGroup = nil }},
			{"nil Run", func(d *AccountDependencies) { d.Run = nil }},
		} {
			t.Run(tc.name, func(t *testing.T) {
				deps := base
				tc.mutate(&deps)
				_, err := EnsureAccount(deps)
				if err == nil || !strings.Contains(err.Error(), "incomplete") {
					t.Fatalf("expected incomplete error, got: %v", err)
				}
			})
		}
	})

	t.Run("fallback group and user creation", func(t *testing.T) {
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
				if name == "addgroup" {
					groupReady = true
				}
				if name == "adduser" {
					userReady = true
				}
				if name == "groupadd" || name == "useradd" {
					return errors.New("primary command failed")
				}
				return nil
			},
		}
		id, err := EnsureAccount(deps)
		if err != nil {
			t.Fatalf("ensure account: %v", err)
		}
		if id.UID != 4242 || id.GID != 4242 {
			t.Fatalf("identity=%+v", id)
		}
		want := []string{
			"groupadd --system veil",
			"addgroup -S veil",
			"useradd --system --gid veil --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin veil",
			"adduser -S -D -H -h /nonexistent -s /sbin/nologin -G veil veil",
		}
		if strings.Join(commands, "\n") != strings.Join(want, "\n") {
			t.Fatalf("commands:\n%s\nwant:\n%s", strings.Join(commands, "\n"), strings.Join(want, "\n"))
		}
	})

	t.Run("group creation fails", func(t *testing.T) {
		deps := AccountDependencies{
			LookupGroup: func(string) (*user.Group, error) { return nil, errors.New("missing") },
			LookupUser:  func(string) (*user.User, error) { return nil, errors.New("missing") },
			Run:         func(string, ...string) error { return errors.New("always fails") },
		}
		_, err := EnsureAccount(deps)
		if err == nil || !strings.Contains(err.Error(), "create veil group") {
			t.Fatalf("expected group creation error, got: %v", err)
		}
	})

	t.Run("group created but unresolved", func(t *testing.T) {
		deps := AccountDependencies{
			LookupGroup: func(string) (*user.Group, error) { return nil, errors.New("missing") },
			LookupUser:  func(string) (*user.User, error) { return nil, errors.New("missing") },
			Run:         func(string, ...string) error { return nil },
		}
		_, err := EnsureAccount(deps)
		if err == nil || !strings.Contains(err.Error(), "resolve created veil group") {
			t.Fatalf("expected resolve group error, got: %v", err)
		}
	})

	t.Run("user creation fails", func(t *testing.T) {
		deps := AccountDependencies{
			LookupGroup: func(string) (*user.Group, error) { return &user.Group{Name: "veil", Gid: "100"}, nil },
			LookupUser:  func(string) (*user.User, error) { return nil, errors.New("missing") },
			Run:         func(string, ...string) error { return errors.New("always fails") },
		}
		_, err := EnsureAccount(deps)
		if err == nil || !strings.Contains(err.Error(), "create veil user") {
			t.Fatalf("expected user creation error, got: %v", err)
		}
	})

	t.Run("user created but unresolved", func(t *testing.T) {
		deps := AccountDependencies{
			LookupGroup: func(string) (*user.Group, error) { return &user.Group{Name: "veil", Gid: "100"}, nil },
			LookupUser:  func(string) (*user.User, error) { return nil, errors.New("missing") },
			Run:         func(string, ...string) error { return nil },
		}
		_, err := EnsureAccount(deps)
		if err == nil || !strings.Contains(err.Error(), "resolve created veil user") {
			t.Fatalf("expected resolve user error, got: %v", err)
		}
	})

	t.Run("invalid uid", func(t *testing.T) {
		deps := AccountDependencies{
			LookupGroup: func(string) (*user.Group, error) { return &user.Group{Name: "veil", Gid: "100"}, nil },
			LookupUser: func(string) (*user.User, error) {
				return &user.User{Username: "veil", Uid: "not-a-number", Gid: "100"}, nil
			},
			Run: func(string, ...string) error { return nil },
		}
		_, err := EnsureAccount(deps)
		if err == nil || !strings.Contains(err.Error(), "parse veil uid") {
			t.Fatalf("expected parse uid error, got: %v", err)
		}
	})

	t.Run("invalid gid", func(t *testing.T) {
		deps := AccountDependencies{
			LookupGroup: func(string) (*user.Group, error) { return &user.Group{Name: "veil", Gid: "not-a-number"}, nil },
			LookupUser: func(string) (*user.User, error) {
				return &user.User{Username: "veil", Uid: "100", Gid: "not-a-number"}, nil
			},
			Run: func(string, ...string) error { return nil },
		}
		_, err := EnsureAccount(deps)
		if err == nil || !strings.Contains(err.Error(), "parse veil gid") {
			t.Fatalf("expected parse gid error, got: %v", err)
		}
	})

	t.Run("gid mismatch", func(t *testing.T) {
		deps := AccountDependencies{
			LookupGroup: func(string) (*user.Group, error) { return &user.Group{Name: "veil", Gid: "100"}, nil },
			LookupUser:  func(string) (*user.User, error) { return &user.User{Username: "veil", Uid: "100", Gid: "200"}, nil },
			Run:         func(string, ...string) error { return nil },
		}
		_, err := EnsureAccount(deps)
		if err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("expected gid mismatch error, got: %v", err)
		}
	})
}

func TestMigrateErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership and mode test")
	}

	t.Run("empty etc dir", func(t *testing.T) {
		err := Migrate(Paths{VarDir: "/tmp"}, Identity{}, nil)
		if err == nil || !strings.Contains(err.Error(), "etc and var directories are required") {
			t.Fatalf("expected required dirs error, got: %v", err)
		}
	})

	t.Run("empty var dir", func(t *testing.T) {
		err := Migrate(Paths{EtcDir: "/tmp"}, Identity{}, nil)
		if err == nil || !strings.Contains(err.Error(), "etc and var directories are required") {
			t.Fatalf("expected required dirs error, got: %v", err)
		}
	})

	t.Run("nil now defaults to time.Now", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "veil")
		if err := os.MkdirAll(etcDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(varDir, 0o755); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		if err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, nil); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	})

	t.Run("symlink in managed var tree", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "veil")
		if err := os.MkdirAll(filepath.Join(varDir, "audit"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/etc", filepath.Join(varDir, "audit", "link")); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, time.Now)
		if err == nil || !strings.Contains(err.Error(), "refuse to migrate symlink") {
			t.Fatalf("expected symlink error, got: %v", err)
		}
	})

	t.Run("non-regular managed var file", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "veil")
		if err := os.MkdirAll(varDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(varDir, "state.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, time.Now)
		if err == nil || !strings.Contains(err.Error(), "non-regular") {
			t.Fatalf("expected non-regular error, got: %v", err)
		}
	})

	t.Run("mkdir blocked by existing file", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "veil")
		if err := os.MkdirAll(varDir, 0o755); err != nil {
			t.Fatal(err)
		}
		// Place a file where Migrate expects to create the "audit" directory.
		if err := os.WriteFile(filepath.Join(varDir, "audit"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, time.Now)
		if err == nil {
			t.Fatal("expected mkdir error")
		}
	})

	t.Run("etc dir creation fails", func(t *testing.T) {
		root := t.TempDir()
		// Create a file named "etc" so MkdirAll for etc/veil fails.
		if err := os.WriteFile(filepath.Join(root, "etc"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "veil")
		err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir}, Identity{}, time.Now)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("var dir creation fails", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc", "veil")
		if err := os.WriteFile(filepath.Join(root, "var"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		varDir := filepath.Join(root, "var", "veil")
		err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir}, Identity{}, time.Now)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("blocked www directory", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "veil")
		if err := os.MkdirAll(varDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(varDir, "www"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, time.Now)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("blocked backups directory", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "veil")
		if err := os.MkdirAll(varDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"audit", "staging", "updates", "autocert", "www"} {
			if err := os.MkdirAll(filepath.Join(varDir, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(varDir, "backups"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, time.Now)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("blocked generated directory", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "veil")
		if err := os.MkdirAll(varDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"audit", "staging", "updates", "autocert", "www", "backups", "promotion-backups", "migration-backups"} {
			if err := os.MkdirAll(filepath.Join(varDir, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(etcDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(etcDir, "generated"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, time.Now)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("blocked tls directory", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "veil")
		if err := os.MkdirAll(varDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"audit", "staging", "updates", "autocert", "www", "backups", "promotion-backups", "migration-backups"} {
			if err := os.MkdirAll(filepath.Join(varDir, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(filepath.Join(etcDir, "generated"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(etcDir, "tls"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, time.Now)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("blocked panel directory", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "veil")
		if err := os.MkdirAll(varDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"audit", "staging", "updates", "autocert", "www", "backups", "promotion-backups", "migration-backups"} {
			if err := os.MkdirAll(filepath.Join(varDir, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range []string{"generated", "tls"} {
			if err := os.MkdirAll(filepath.Join(etcDir, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(etcDir, "panel"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, time.Now)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("state.json symlink rejected", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "veil")
		if err := os.MkdirAll(varDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"audit", "staging", "updates", "autocert", "www", "backups", "promotion-backups", "migration-backups"} {
			if err := os.MkdirAll(filepath.Join(varDir, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range []string{"generated", "tls", "panel"} {
			if err := os.MkdirAll(filepath.Join(etcDir, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink("/etc", filepath.Join(varDir, "state.json")); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, time.Now)
		if err == nil || !strings.Contains(err.Error(), "non-regular managed file") {
			t.Fatalf("expected non-regular error, got: %v", err)
		}
	})

	t.Run("state.key symlink rejected", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc", "veil")
		varDir := filepath.Join(root, "var", "veil")
		if err := os.MkdirAll(varDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"audit", "staging", "updates", "autocert", "www", "backups", "promotion-backups", "migration-backups"} {
			if err := os.MkdirAll(filepath.Join(varDir, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range []string{"generated", "tls", "panel"} {
			if err := os.MkdirAll(filepath.Join(etcDir, name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(varDir, "state.json"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(varDir, "sessions.json"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/etc", filepath.Join(etcDir, "state.key")); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		err := Migrate(Paths{EtcDir: etcDir, VarDir: varDir, RootUID: uid, RootGID: gid}, Identity{UID: uid, GID: gid}, time.Now)
		if err == nil || !strings.Contains(err.Error(), "non-regular managed file") {
			t.Fatalf("expected non-regular error, got: %v", err)
		}
	})
}

func TestCreateSafetyCopies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership and mode test")
	}

	t.Run("no managed files", func(t *testing.T) {
		root := t.TempDir()
		paths := Paths{EtcDir: filepath.Join(root, "etc"), VarDir: filepath.Join(root, "var")}
		safety, err := createSafetyCopies(paths, time.Now())
		if err != nil {
			t.Fatalf("create safety copies: %v", err)
		}
		if safety != "" {
			t.Fatalf("expected empty safety root, got %q", safety)
		}
	})

	t.Run("copies existing files", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc")
		varDir := filepath.Join(root, "var")
		for _, path := range []string{
			filepath.Join(etcDir, "state.key"),
			filepath.Join(etcDir, "veil.env"),
			filepath.Join(varDir, "state.json"),
			filepath.Join(varDir, "sessions.json"),
		} {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		paths := Paths{EtcDir: etcDir, VarDir: varDir, RootUID: os.Getuid(), RootGID: os.Getgid()}
		safety, err := createSafetyCopies(paths, now)
		if err != nil {
			t.Fatalf("create safety copies: %v", err)
		}
		wantBase := filepath.Join(varDir, "migration-backups", "20260605T120000Z")
		if safety != wantBase {
			t.Fatalf("safety root=%q want=%q", safety, wantBase)
		}
		for _, name := range []string{"state.key", "veil.env", "state.json", "sessions.json"} {
			data, err := os.ReadFile(filepath.Join(safety, name))
			if err != nil {
				t.Fatalf("read safety copy %s: %v", name, err)
			}
			if string(data) != name {
				t.Fatalf("safety copy %s content=%q want=%q", name, string(data), name)
			}
		}
	})

	t.Run("suffix collision", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc")
		varDir := filepath.Join(root, "var")
		if err := os.MkdirAll(etcDir, 0o755); err != nil {
			t.Fatal(err)
		}
		key := filepath.Join(etcDir, "state.key")
		if err := os.WriteFile(key, []byte("key"), 0o644); err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
		paths := Paths{EtcDir: etcDir, VarDir: varDir, RootUID: os.Getuid(), RootGID: os.Getgid()}
		if _, err := createSafetyCopies(paths, now); err != nil {
			t.Fatal(err)
		}
		if _, err := createSafetyCopies(paths, now); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(varDir, "migration-backups", "20260605T120000Z-1")); err != nil {
			t.Fatalf("expected suffix collision dir: %v", err)
		}
	})

	t.Run("symlink source rejected", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc")
		varDir := filepath.Join(root, "var")
		if err := os.MkdirAll(etcDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/etc", filepath.Join(etcDir, "state.key")); err != nil {
			t.Fatal(err)
		}
		paths := Paths{EtcDir: etcDir, VarDir: varDir}
		_, err := createSafetyCopies(paths, time.Now())
		if err == nil || !strings.Contains(err.Error(), "refuse to migrate non-regular managed file") {
			t.Fatalf("expected non-regular error, got: %v", err)
		}
	})

	t.Run("migration-backups file blocks directory", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc")
		varDir := filepath.Join(root, "var")
		if err := os.MkdirAll(etcDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(etcDir, "state.key"), []byte("key"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(varDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(varDir, "migration-backups"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths := Paths{EtcDir: etcDir, VarDir: varDir, RootUID: os.Getuid(), RootGID: os.Getgid()}
		_, err := createSafetyCopies(paths, time.Now())
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("path too long for backup directory", func(t *testing.T) {
		root := t.TempDir()
		etcDir := filepath.Join(root, "etc")
		if err := os.MkdirAll(etcDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(etcDir, "state.key"), []byte("key"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Build a deeply-nested varDir whose full length is valid but whose backup
		// timestamp candidate exceeds PATH_MAX, causing os.Mkdir to fail with a
		// non-IsExist error inside the suffix loop.
		wantLen := 4065
		varDir := root
		for len(varDir) < wantLen-256 {
			varDir = filepath.Join(varDir, strings.Repeat("x", 200))
		}
		if remaining := wantLen - len(varDir) - 1; remaining > 0 {
			varDir = filepath.Join(varDir, strings.Repeat("x", remaining))
		}
		paths := Paths{EtcDir: etcDir, VarDir: varDir, RootUID: os.Getuid(), RootGID: os.Getgid()}
		_, err := createSafetyCopies(paths, time.Now())
		if err == nil {
			t.Fatal("expected error for path too long")
		}
	})
}

func TestCopyRegularFile(t *testing.T) {
	t.Run("missing source", func(t *testing.T) {
		err := copyRegularFile(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "dst"))
		if err == nil {
			t.Fatal("expected error for missing source")
		}
	})

	t.Run("destination exists", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "src")
		dst := filepath.Join(t.TempDir(), "dst")
		if err := os.WriteFile(src, []byte("src"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte("dst"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := copyRegularFile(src, dst)
		if err == nil {
			t.Fatal("expected error for existing destination")
		}
	})

	t.Run("copies content", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "src")
		dst := filepath.Join(t.TempDir(), "dst")
		content := []byte("hello world")
		if err := os.WriteFile(src, content, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := copyRegularFile(src, dst); err != nil {
			t.Fatalf("copy: %v", err)
		}
		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(content) {
			t.Fatalf("content=%q want=%q", got, content)
		}
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode=%#o want=%#o", info.Mode().Perm(), 0o600)
		}
	})
}

func TestEnsureOwnedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership and mode test")
	}

	t.Run("creates and owns directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "owned")
		uid, gid := os.Getuid(), os.Getgid()
		if err := ensureOwnedDirectory(path, 0o700, uid, gid); err != nil {
			t.Fatalf("ensure owned directory: %v", err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("mode=%#o want=%#o", info.Mode().Perm(), 0o700)
		}
	})

	t.Run("mkdirall blocked by file parent", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, "blocker")
		if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := ensureOwnedDirectory(filepath.Join(blocker, "child"), 0o755, os.Getuid(), os.Getgid())
		if err == nil {
			t.Fatal("expected mkdirall error")
		}
	})

}

func TestApplyTreeOwnership(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership and mode test")
	}

	t.Run("applies ownership recursively", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "tree")
		if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		if err := applyTreeOwnership(dir, 0o700, 0o600, uid, gid); err != nil {
			t.Fatalf("apply tree ownership: %v", err)
		}
		assertMode(t, dir, 0o700)
		assertMode(t, filepath.Join(dir, "nested"), 0o700)
		assertMode(t, filepath.Join(dir, "file"), 0o600)
	})

	t.Run("symlink in tree rejected", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "tree")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/etc", filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}
		err := applyTreeOwnership(dir, 0o700, 0o600, os.Getuid(), os.Getgid())
		if err == nil || !strings.Contains(err.Error(), "refuse to migrate symlink") {
			t.Fatalf("expected symlink error, got: %v", err)
		}
	})

	t.Run("non-regular path rejected", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX test")
		}
		root := t.TempDir()
		dir := filepath.Join(root, "tree")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := applyTreeOwnership(dir, 0o700, 0o600, os.Getuid(), os.Getgid())
		if err == nil || !strings.Contains(err.Error(), "refuse to migrate non-regular path") {
			t.Fatalf("expected non-regular error, got: %v", err)
		}
	})
}

func TestSetOptionalFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX ownership and mode test")
	}

	t.Run("missing file ignored", func(t *testing.T) {
		err := setOptionalFile(filepath.Join(t.TempDir(), "missing"), 0o600, os.Getuid(), os.Getgid())
		if err != nil {
			t.Fatalf("set optional file: %v", err)
		}
	})

	t.Run("sets mode and ownership", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(path, []byte("x"), 0o666); err != nil {
			t.Fatal(err)
		}
		uid, gid := os.Getuid(), os.Getgid()
		if err := setOptionalFile(path, 0o640, uid, gid); err != nil {
			t.Fatalf("set optional file: %v", err)
		}
		assertMode(t, path, 0o640)
	})

	t.Run("symlink rejected", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink("/etc", link); err != nil {
			t.Fatal(err)
		}
		err := setOptionalFile(link, 0o600, os.Getuid(), os.Getgid())
		if err == nil || !strings.Contains(err.Error(), "non-regular managed file") {
			t.Fatalf("expected non-regular error, got: %v", err)
		}
	})

}
