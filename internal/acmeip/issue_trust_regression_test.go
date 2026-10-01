package acmeip

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #1226: an existing acme.sh is trusted only while it is a root-owned,
// non-symlink, non-group/other-writable file inside equally protected parent
// directories AND byte-identical to the pinned release payload. Anything else
// is removed/healed and reinstalled — "the file exists" is never enough for
// root to exec it.
func trustGateSystem() (*fakeSystem, string) {
	sys := newFakeSystem()
	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key("sh", "-c", acmeShInstallScript())] = commandResult{out: "installed"}
	return sys, acmeSh
}

func installRan(sys *fakeSystem) bool {
	for _, call := range sys.execCalls {
		if strings.HasPrefix(call, "sh -c ") && strings.Contains(call, acmeShTarballURL) {
			return true
		}
	}
	return false
}

func rmCallsFor(sys *fakeSystem, path string) bool {
	for _, call := range sys.runCalls {
		if len(call) == 3 && call[0] == "rm" && call[1] == "-rf" && call[2] == path {
			return true
		}
	}
	return false
}

func TestEnsureAcmeShRejectsSymlinkedScript(t *testing.T) {
	sys, acmeSh := trustGateSystem()
	sys.files[acmeSh] = &fakeFileInfo{name: "acme.sh", mode: os.ModeSymlink | 0o755}
	sys.fileData[acmeSh] = fakeAcmeShScript

	if _, err := ensureAcmeSh(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rmCallsFor(sys, filepath.Dir(acmeSh)) {
		t.Fatalf("planted payload tree must be removed before reinstall: %v", sys.runCalls)
	}
	if !installRan(sys) {
		t.Fatal("untrusted acme.sh must trigger a reinstall")
	}
}

func TestEnsureAcmeShRejectsWritableScript(t *testing.T) {
	sys, acmeSh := trustGateSystem()
	sys.files[acmeSh] = &fakeFileInfo{name: "acme.sh", mode: 0o777, sys: fakeSysStat(0, 0)}
	sys.fileData[acmeSh] = fakeAcmeShScript

	if _, err := ensureAcmeSh(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !installRan(sys) {
		t.Fatal("group/other-writable acme.sh must trigger a reinstall")
	}
}

func TestEnsureAcmeShRejectsDigestMismatch(t *testing.T) {
	sys, acmeSh := trustGateSystem()
	sys.files[acmeSh] = &fakeFileInfo{name: "acme.sh", mode: 0o755, sys: fakeSysStat(0, 0)}
	sys.fileData[acmeSh] = []byte("#!/bin/sh\n# planted payload\n")

	if _, err := ensureAcmeSh(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !installRan(sys) {
		t.Fatal("a payload differing from the pinned release digest must reinstall")
	}
}

func TestEnsureAcmeShHealsWritableHome(t *testing.T) {
	sys, _ := trustGateSystem()
	sys.files[sys.home] = &fakeFileInfo{name: filepath.Base(sys.home), mode: os.ModeDir | 0o777, sys: fakeSysStat(0, 0)}

	if _, err := ensureAcmeSh(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := sys.files[sys.home].mode.Perm(); got != 0o700 {
		t.Fatalf("writable acme home must be healed to 0700, got %o", got)
	}
	if !installRan(sys) {
		t.Fatal("untrusted acme home must trigger a reinstall")
	}
}

func TestEnsureAcmeShRemovesNonDirectoryHome(t *testing.T) {
	sys, _ := trustGateSystem()
	acmeDir := filepath.Join(sys.home, ".acme.sh")
	sys.files[acmeDir] = &fakeFileInfo{name: ".acme.sh", mode: os.ModeSymlink | 0o777}

	if _, err := ensureAcmeSh(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !rmCallsFor(sys, acmeDir) {
		t.Fatalf("non-directory .acme.sh path must be removed: %v", sys.runCalls)
	}
	if !installRan(sys) {
		t.Fatal("a symlinked .acme.sh dir must trigger a reinstall")
	}
}

func TestEnsureAcmeShTrustedSkipsInstall(t *testing.T) {
	sys, _ := trustGateSystem()
	sys.setAcmeInstalled()

	if _, err := ensureAcmeSh(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if installRan(sys) {
		t.Fatal("a fully trusted acme.sh must not reinstall")
	}
}

// A compromised install that "succeeds" but leaves a non-pinned payload must
// still fail closed — the post-install revalidation is the last gate before
// root exec's the script.
func TestEnsureAcmeShFailsClosedOnForgedInstall(t *testing.T) {
	sys, acmeSh := trustGateSystem()
	sys.commands[sys.key("sh", "-c", acmeShInstallScript())] = commandResult{
		out: "installed",
		writeOwned: func() {
			sys.files[acmeSh] = &fakeFileInfo{name: "acme.sh", mode: 0o755, sys: fakeSysStat(0, 0)}
			sys.fileData[acmeSh] = []byte("#!/bin/sh\n# forged post-install payload\n")
		},
	}
	sys.installAcmeSh = false

	if _, err := ensureAcmeSh(context.Background(), sys); err == nil {
		t.Fatal("an install leaving an untrusted acme.sh must fail closed")
	}
}
