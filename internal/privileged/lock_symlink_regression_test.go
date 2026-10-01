package privileged

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

// Regression for #1229-F5: every privileged lock file used to be opened
// O_CREATE|O_RDWR by path, so a symlinked leaf inside the service-writable
// state tree redirected a root-owned open at an attacker-chosen file. The
// locks are now created relative to a pinned directory descriptor with
// O_NOFOLLOW: a planted symlink fails closed and the target is never created.

func TestFenceLockRejectsSymlinkedLockFile(t *testing.T) {
	root := t.TempDir()
	fencePath := filepath.Join(root, "transactions", "runtime-fence.json")
	if err := os.MkdirAll(filepath.Dir(fencePath), 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(root, "victim")
	if err := os.Symlink(victim, fencePath+".lock"); err != nil {
		t.Fatal(err)
	}
	guard := newFenceGuard(fencePath, true)
	err := guard.Accept(FenceToken{
		Owner: "test", Generation: 1, OperationID: "op",
		LeaseExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	if err == nil {
		t.Fatal("fence accept followed a symlinked lock file")
	}
	if _, statErr := os.Lstat(victim); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("symlinked lock opened %s: %v", victim, statErr)
	}
}

func TestPromotionLockRejectsSymlinkedLockFile(t *testing.T) {
	root := t.TempDir()
	backupRoot := filepath.Join(root, "backups")
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(root, "victim")
	if err := os.Symlink(victim, filepath.Join(backupRoot, ".promotion.lock")); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "staged.json")
	writeFileWithParents(t, source, []byte("new"))
	_, err := executePromotionTransaction(backupRoot, time.Now, "promotion", []ResolvedArtifact{{
		ID: "cfg", Source: source, Destination: filepath.Join(root, "live.json"),
	}}, nil)
	if err == nil {
		t.Fatal("promotion followed a symlinked lock file")
	}
	if _, statErr := os.Lstat(victim); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("symlinked lock opened %s: %v", victim, statErr)
	}
}

func TestPromotionLockRejectsSymlinkedBackupRoot(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked")
	if err := os.Symlink(real, linked); err != nil {
		t.Fatal(err)
	}
	_, err := executePromotionTransaction(linked, time.Now, "promotion", []ResolvedArtifact{{
		ID: "cfg", Source: filepath.Join(root, "staged.json"), Destination: filepath.Join(root, "live.json"),
	}}, nil)
	if err == nil {
		t.Fatal("promotion followed a symlinked backup root")
	}
}

func TestFirewallLockRejectsSymlinkedLockFile(t *testing.T) {
	root := firewallRoot0700(t)
	victim := filepath.Join(root, "victim")
	if err := os.Symlink(victim, filepath.Join(root, firewallLockName)); err != nil {
		t.Fatal(err)
	}
	_, err := withFirewallLock(root, func(*safefs.Dir) (FirewallResult, error) {
		return FirewallResult{}, nil
	})
	if !errors.Is(err, unix.ELOOP) {
		t.Fatalf("firewall lock open error = %v, want ELOOP from the symlinked leaf", err)
	}
	if _, statErr := os.Lstat(victim); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("symlinked lock opened %s: %v", victim, statErr)
	}
}
