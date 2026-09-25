package hostaccess

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Regression for #1009: Migrate ran lstat-then-chmod/chown, so a path swapped
// for a symlink between the check and the operation was followed as root —
// chmod/chown landing on an attacker-chosen target outside the managed tree.
// The managed mutations must operate on an O_NOFOLLOW descriptor, which
// refuses the link outright. These assertions go through testHooks.chmod /
// testHooks.chown / copyRegularFile — the exact call sites Migrate uses — so
// they fail on the pre-fix wiring (plain os.Chmod/os.Chown/os.Open follow the
// link) and not just because new helpers exist.
func TestManagedMutationHooksRejectSwappedSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test")
	}
	root := t.TempDir()
	target := filepath.Join(root, "outside-target")
	if err := os.WriteFile(target, []byte("sensitive"), 0o600); err != nil {
		t.Fatal(err)
	}
	targetInfo, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, swapped := range []string{"managed-file", "managed-dir"} {
		name := swapped
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, name)
			if name == "managed-dir" {
				if err := os.Symlink(root, path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			} else if err := os.Symlink(target, path); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if err := testHooks.chmod(path, 0o777); err == nil {
				t.Fatalf("chmod followed symlink %s", path)
			}
			// chown to a different uid so a followed link visibly mutates the
			// target; pre-fix (os.Chown) this succeeds when running as root.
			if err := testHooks.chown(path, os.Getuid()+1, os.Getgid()+1); err == nil {
				t.Fatalf("chown followed symlink %s", path)
			}
			// The link target's mode must be untouched.
			info, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("chmod landed on the symlink target: mode=%o", info.Mode().Perm())
			}
			// Restore any ownership drift so the parent tempdir cleanup works
			// on hosts where the pre-fix chown succeeded.
			if info.Sys() != targetInfo.Sys() {
				_ = os.Chown(target, os.Getuid(), os.Getgid())
			}
		})
	}
}

// #1009 companion: the safety-copy read path had the same lstat-then-open
// window — a swapped symlink was opened and its target copied as root.
func TestCopyRegularFileRejectsSwappedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test")
	}
	root := t.TempDir()
	secret := filepath.Join(root, "root-secret")
	if err := os.WriteFile(secret, []byte("root-only-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "managed-copy-source")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	dst := filepath.Join(root, "copied")
	if err := copyRegularFile(link, dst); err == nil {
		body, _ := os.ReadFile(dst)
		t.Fatalf("copyRegularFile followed a symlink; copied %q", body)
	}
}

// #1009 companion: the same descriptor path still operates on real files, so
// the hardening cannot silently break migration. Runs through the testHooks
// wiring so it also passes pre-fix — it guards behavior, not the mechanism.
func TestManagedNoFollowOperationsActOnRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics test")
	}
	path := filepath.Join(t.TempDir(), "managed")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := testHooks.chmod(path, 0o640); err != nil {
		t.Fatalf("chmod on regular file: %v", err)
	}
	if err := testHooks.chown(path, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("chown on regular file: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o, want 640", info.Mode().Perm())
	}
}
