//go:build linux || darwin || freebsd || netbsd

package safefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FileIdentity must yield the dev:ino:ctime tag on every platform that can
// supply it (#1243: the Linux-only Stat_t.Ctim access broke darwin/freebsd
// builds), and the tag must distinguish a replaced file from the original.
func TestFileIdentityTracksInodeReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary")
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	tag, ok := FileIdentity(first)
	if !ok {
		t.Fatal("FileIdentity reported no identity on a platform that supplies one")
	}
	if parts := strings.Split(tag, ":"); len(parts) != 4 {
		t.Fatalf("identity %q is not dev:ino:ctimeSec:ctimeNsec", tag)
	}
	again, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if tagAgain, _ := FileIdentity(again); tagAgain != tag {
		t.Fatalf("identity of an untouched file changed: %q -> %q", tag, tagAgain)
	}

	// Keep the original inode alive under another name so the replacement
	// cannot recycle its inode number.
	if err := os.Rename(path, filepath.Join(dir, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	replaced, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacedTag, ok := FileIdentity(replaced)
	if !ok || replacedTag == tag {
		t.Fatalf("replaced file identity = %q (ok=%v), want a tag different from %q", replacedTag, ok, tag)
	}
}

func TestFileIdentityRejectsForeignSys(t *testing.T) {
	if tag, ok := FileIdentity(fakeInfo{}); ok || tag != "" {
		t.Fatalf("FileIdentity(non-Stat_t) = %q, %v; want \"\", false", tag, ok)
	}
}

type fakeInfo struct{ os.FileInfo }

func (fakeInfo) Sys() any { return fmt.Sprint("not a stat") }
