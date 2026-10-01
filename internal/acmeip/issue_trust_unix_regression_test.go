//go:build unix

package acmeip

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Issue #1226: ownership checks only engage when the caller is root — run
// them under a mocked uid 0 on unix, where FileInfo.Sys carries uid/gid.
func TestEnsureAcmeShRejectsForeignOwnedScript(t *testing.T) {
	orig := getuidFunc
	getuidFunc = func() int { return 0 }
	defer func() { getuidFunc = orig }()

	sys, acmeSh := trustGateSystem()
	sys.files[acmeSh] = &fakeFileInfo{name: "acme.sh", mode: 0o755, sys: fakeSysStat(1000, 1000)}
	sys.fileData[acmeSh] = fakeAcmeShScript

	if _, err := ensureAcmeSh(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !installRan(sys) {
		t.Fatal("non-root-owned acme.sh must trigger a reinstall")
	}
}

func TestEnsureAcmeShHealsForeignOwnedHome(t *testing.T) {
	orig := getuidFunc
	getuidFunc = func() int { return 0 }
	defer func() { getuidFunc = orig }()

	sys, _ := trustGateSystem()
	sys.files[sys.home] = &fakeFileInfo{name: filepath.Base(sys.home), mode: os.ModeDir | 0o700, sys: fakeSysStat(1000, 1000)}

	if _, err := ensureAcmeSh(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var healed bool
	for _, c := range sys.chownCalls {
		if c.name == sys.home && c.uid == 0 {
			healed = true
		}
	}
	if !healed {
		t.Fatalf("foreign-owned acme home must be chown'd to root: %v", sys.chownCalls)
	}
}
