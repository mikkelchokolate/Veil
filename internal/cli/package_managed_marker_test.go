package cli

import (
	"os"
	"strings"
	"testing"
)

// TestPackagingWritesThePackageManagedMarker locks #1424's packaging half: the
// package's postinstall marks the install so the updater refuses to replace the
// package-owned binary, and postremove drops the marker with the package.
func TestPackagingWritesThePackageManagedMarker(t *testing.T) {
	postinstall, err := os.ReadFile("../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	code := stripHashComments(t, strings.ReplaceAll(string(postinstall), "\r\n", "\n"))
	if !strings.Contains(code, "/etc/veil/.package-managed") {
		t.Fatalf("postinstall does not write the package-managed marker:\n%s", code)
	}
	postremove, err := os.ReadFile("../../packaging/scripts/postremove.sh")
	if err != nil {
		t.Fatal(err)
	}
	removeCode := stripHashComments(t, strings.ReplaceAll(string(postremove), "\r\n", "\n"))
	if !strings.Contains(removeCode, "rm -f /etc/veil/.package-managed") {
		t.Fatalf("postremove keeps the package-managed marker:\n%s", removeCode)
	}
}
