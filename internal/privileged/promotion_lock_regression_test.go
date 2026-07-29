package privileged

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestPromotionLockCoversPreflightThroughPublicationAcrossProcesses(t *testing.T) {
	root := t.TempDir()
	backupRoot := filepath.Join(root, "promotion-backups")
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source.json")
	destination := filepath.Join(root, "live.json")
	done := filepath.Join(root, "done")
	if err := os.WriteFile(source, []byte("version-before-lock-release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(backupRoot, ".promotion.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		}
	}()

	command := exec.Command(os.Args[0], "-test.run=^TestPromotionLockSubprocessHelper$")
	command.Env = append(os.Environ(),
		"VEIL_PROMOTION_LOCK_HELPER=1",
		"VEIL_PROMOTION_BACKUP_ROOT="+backupRoot,
		"VEIL_PROMOTION_SOURCE="+source,
		"VEIL_PROMOTION_DESTINATION="+destination,
		"VEIL_PROMOTION_DONE="+done,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
	}()

	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(done); err == nil {
		_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		locked = false
		_ = command.Wait()
		t.Fatal("promotion completed while another process held the shared promotion lock")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("version-after-lock-release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	locked = false
	if err := command.Wait(); err != nil {
		t.Fatalf("promotion subprocess: %v", err)
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "version-after-lock-release" {
		t.Fatalf("promotion preflight escaped lock: live=%q", body)
	}
	if _, err := os.Stat(filepath.Join(backupRoot, promotionTransactionJournalName)); !os.IsNotExist(err) {
		t.Fatalf("promotion journal remains after serialized commit: %v", err)
	}
}

func TestPromotionLockSubprocessHelper(t *testing.T) {
	if os.Getenv("VEIL_PROMOTION_LOCK_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	result, err := executePromotionTransaction(os.Getenv("VEIL_PROMOTION_BACKUP_ROOT"), time.Now, "promotion", []ResolvedArtifact{{
		ID: "caddy/config.json", Source: os.Getenv("VEIL_PROMOTION_SOURCE"), Destination: os.Getenv("VEIL_PROMOTION_DESTINATION"),
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.WrittenArtifacts) != 1 {
		t.Fatalf("unexpected promotion result: %+v", result)
	}
	if err := os.WriteFile(os.Getenv("VEIL_PROMOTION_DONE"), []byte("done"), 0o600); err != nil {
		t.Fatal(err)
	}
}
