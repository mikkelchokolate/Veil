package backup

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

// TestFileOwnerIDsDecodesPinnedStatMetadata locks the #1254 review finding that
// safefs handles adapt fstatat into *unix.Stat_t: decoding only *syscall.Stat_t
// reported 0:0 for every pinned entry, so a restore chown'ed the destination to
// root instead of preserving its owner.
func TestFileOwnerIDsDecodesPinnedStatMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	dirPath := t.TempDir()
	target := filepath.Join(dirPath, "state.json")
	if err := os.WriteFile(target, []byte("state"), 0o640); err != nil {
		t.Fatal(err)
	}
	dir, err := safefs.OpenDir(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	info, err := dir.StatAt("state.json")
	if err != nil {
		t.Fatal(err)
	}
	uid, gid, ok := fileOwnerIDs(info)
	if !ok {
		t.Fatalf("pinned metadata carries no owner: %T", info.Sys())
	}
	byPath, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	wantUID, wantGID, ok := fileOwnerIDs(byPath)
	if !ok {
		t.Fatalf("lstat metadata carries no owner: %T", byPath.Sys())
	}
	if uid != wantUID || gid != wantGID {
		t.Fatalf("pinned owner = %d:%d, lstat owner = %d:%d", uid, gid, wantUID, wantGID)
	}
}

// TestUnsafeJournalHardLinkSeesPinnedStatLinkCount locks the same review
// finding for the #1219 hard-link guard: on a pinned entry the old check always
// answered "safe", so a multi-linked journal member could alias another file.
func TestUnsafeJournalHardLinkSeesPinnedStatLinkCount(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	dirPath := t.TempDir()
	target := filepath.Join(dirPath, "journal")
	if err := os.WriteFile(target, []byte("journal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, filepath.Join(dirPath, "journal-alias")); err != nil {
		// No skip: the CI test inventory rejects skips that are not on its
		// allowlist. This filesystem cannot express the case at all.
		t.Logf("hard links unsupported here: %v", err)
		return
	}
	dir, err := safefs.OpenDir(dirPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	info, err := dir.StatAt("journal")
	if err != nil {
		t.Fatal(err)
	}
	if !unsafeJournalHardLink(info) {
		t.Fatalf("pinned multi-linked journal member reported safe (%T)", info.Sys())
	}
}
