//go:build linux

package managedfiles

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// swapAfterLstat installs planBeforeReadHook so swap runs once, after Plan's
// Lstat classified the leaf as a correct regular file and before the read.
func swapAfterLstat(t *testing.T, swap func(path string)) {
	t.Helper()
	planBeforeReadHook = swap
	t.Cleanup(func() { planBeforeReadHook = nil })
}

func planOne(t *testing.T, file File) RepairPlan {
	t.Helper()
	type result struct {
		plan RepairPlan
		err  error
	}
	done := make(chan result, 1)
	go func() {
		plan, err := NewSet([]File{file}).Plan()
		done <- result{plan, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Plan: %v", r.err)
		}
		return r.plan
	case <-time.After(5 * time.Second):
		t.Fatal("Plan blocked on a swapped leaf")
		return RepairPlan{}
	}
}

func wantReason(t *testing.T, plan RepairPlan, want RepairReason) {
	t.Helper()
	if len(plan.Actions) != 1 || plan.Actions[0].Reason != want {
		t.Fatalf("actions = %+v, want one %q action", plan.Actions, want)
	}
}

func writeManaged(t *testing.T, content string) (string, File) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "managed.env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, File{Path: path, Content: content, Mode: 0o600}
}

// A symlink swapped in after Lstat must not be followed: the old
// os.ReadFile compared the attacker's target, which here holds exactly the
// desired content, so the planted link was reported clean and never repaired.
func TestPlanDoesNotFollowSymlinkSwappedAfterLstat(t *testing.T) {
	path, file := writeManaged(t, "SECRET=desired\n")
	decoy := filepath.Join(t.TempDir(), "decoy")
	if err := os.WriteFile(decoy, []byte(file.Content), 0o600); err != nil {
		t.Fatal(err)
	}
	swapAfterLstat(t, func(p string) {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(decoy, p); err != nil {
			t.Fatal(err)
		}
	})
	wantReason(t, planOne(t, file), RepairReasonDrifted)
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("plan must not touch the leaf: info=%v err=%v", info, err)
	}
}

// A FIFO swapped in after Lstat must not wedge the plan (os.ReadFile would
// block in open(2) waiting for a writer).
func TestPlanDoesNotBlockOnFIFOSwappedAfterLstat(t *testing.T) {
	_, file := writeManaged(t, "SECRET=desired\n")
	swapAfterLstat(t, func(p string) {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(p, 0o600); err != nil {
			t.Fatal(err)
		}
	})
	wantReason(t, planOne(t, file), RepairReasonDrifted)
}

// A different regular inode renamed over the leaf is judged by the
// descriptor, not trusted from the earlier Lstat. Content and mode match, so
// only the inode identity check can flag it: the Lstat verdict (mode, owner)
// belonged to another file, and a conservative rewrite is harmless.
func TestPlanFlagsInodeReplacedAfterLstat(t *testing.T) {
	_, file := writeManaged(t, "SECRET=desired\n")
	swapAfterLstat(t, func(p string) {
		replacement := p + ".new"
		if err := os.WriteFile(replacement, []byte(file.Content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(replacement, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, p); err != nil {
			t.Fatal(err)
		}
	})
	wantReason(t, planOne(t, file), RepairReasonDrifted)
}

func TestPlanTreatsLeafRemovedAfterLstatAsMissing(t *testing.T) {
	_, file := writeManaged(t, "SECRET=desired\n")
	swapAfterLstat(t, func(p string) {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	})
	wantReason(t, planOne(t, file), RepairReasonMissing)
}

func TestPlanRejectsDirectorySwappedAfterLstat(t *testing.T) {
	_, file := writeManaged(t, "SECRET=desired\n")
	swapAfterLstat(t, func(p string) {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	})
	_, err := NewSet([]File{file}).Plan()
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("Plan error = %v, want directory refusal", err)
	}
}

// The read is bounded at len(Content)+1: a file that only appends to the
// desired content is still drift, and an exact match is still clean.
func TestPlanBoundedReadComparesExactContent(t *testing.T) {
	_, file := writeManaged(t, "SECRET=desired\n")
	if plan := planOne(t, file); plan.HasChanges() {
		t.Fatalf("exact content planned %+v", plan.Actions)
	}
	if err := os.WriteFile(file.Path, []byte(file.Content+strings.Repeat("X", 1<<20)), 0o600); err != nil {
		t.Fatal(err)
	}
	wantReason(t, planOne(t, file), RepairReasonDrifted)
	if err := os.WriteFile(file.Path, []byte(strings.TrimSuffix(file.Content, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	wantReason(t, planOne(t, file), RepairReasonDrifted)
}
