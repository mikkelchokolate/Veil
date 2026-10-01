package acmeip

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallCertFailureMustNotAcceptOldExpiredPair(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	expiredCert, expiredKey := generateIPCertPEM(time.Now().Add(-time.Hour), "192.0.2.20")
	certPath := "/etc/veil/panel/tls.crt"
	keyPath := "/etc/veil/panel/tls.key"
	sys.fileData[certPath] = expiredCert
	sys.fileData[keyPath] = expiredKey
	sys.files[certPath] = &fakeFileInfo{name: "tls.crt", mode: 0o600}
	sys.files[keyPath] = &fakeFileInfo{name: "tls.key", mode: 0o600}

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "192.0.2.20", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "192.0.2.20", "--key-file", keyPath, "--fullchain-file", certPath, "--reloadcmd", renewReloadCmd(certPath, keyPath))] = commandResult{err: errors.New("installcert failed")}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "192.0.2.20", System: sys})
	if err == nil {
		t.Fatal("installcert failed but IssueIPCert returned success for leftover expired certificate")
	}
	if !bytes.Equal(sys.fileData[certPath], expiredCert) {
		t.Fatal("failure path mutated the preexisting expired certificate")
	}
}

func TestInstallCertFailureWithoutOldCertificateStillErrors(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "192.0.2.20", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "192.0.2.20", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{err: errors.New("installcert failed")}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "192.0.2.20", System: sys})
	if err == nil {
		t.Fatal("expected error when installcert failed and no certificate was written")
	}
}

// Issue #1208: a partial --installcert failure must roll the destination back
// to the predecessor's bytes AND metadata — WriteFile alone would keep
// whatever mode/owner acme.sh left behind (e.g. root:root 0600, which the
// veil-proxy runtime group can no longer read).
func TestInstallCertRollbackRestoresOwnershipAndMode(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	prevCert, prevKey := generateIPCertPEM(time.Now().Add(24*time.Hour), "192.0.2.30")
	certPath := "/etc/veil/panel/tls.crt"
	keyPath := "/etc/veil/panel/tls.key"
	sys.fileData[certPath] = prevCert
	sys.fileData[keyPath] = prevKey
	sys.files[certPath] = &fakeFileInfo{name: "tls.crt", mode: 0o640, sys: fakeSysStat(0, 995)}
	sys.files[keyPath] = &fakeFileInfo{name: "tls.key", mode: 0o640, sys: fakeSysStat(0, 995)}

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "192.0.2.30", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "192.0.2.30", "--key-file", keyPath, "--fullchain-file", certPath, "--reloadcmd", renewReloadCmd(certPath, keyPath))] = commandResult{
		err: errors.New("installcert failed"),
		writeOwned: func() {
			// acme.sh replaced the pair with root-only material before dying.
			sys.fileData[certPath] = []byte("partial cert")
			sys.fileData[keyPath] = []byte("partial key")
			sys.files[certPath] = &fakeFileInfo{name: "tls.crt", mode: 0o600, sys: fakeSysStat(0, 0)}
			sys.files[keyPath] = &fakeFileInfo{name: "tls.key", mode: 0o600, sys: fakeSysStat(0, 0)}
		},
	}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "192.0.2.30", System: sys})
	if err == nil {
		t.Fatal("expected installcert failure")
	}
	for _, path := range []string{certPath, keyPath} {
		fi := sys.files[path]
		if got := fi.Mode().Perm(); got != 0o640 {
			t.Fatalf("rollback left %s mode %o, want 0640", path, got)
		}
		if uid, gid, ok := fileOwnership(fi); ok && (uid != 0 || gid != 995) {
			t.Fatalf("rollback left %s owned %d:%d, want 0:995", path, uid, gid)
		}
	}
	if !bytes.Equal(sys.fileData[certPath], prevCert) || !bytes.Equal(sys.fileData[keyPath], prevKey) {
		t.Fatal("rollback did not restore predecessor bytes")
	}
	var chowned bool
	for _, c := range sys.chownCalls {
		if c.name == certPath && c.uid == 0 && c.gid == 995 {
			chowned = true
		}
	}
	if !chowned {
		t.Fatalf("rollback must chown the predecessor ownership back: %v", sys.chownCalls)
	}
}

// A failed rollback restore must surface — silently leaving acme.sh's
// root-only bytes behind would look like a clean "kept the old cert" failure
// while the panel actually reads a broken pair (#1208).
func TestInstallCertRollbackFailurePropagates(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	prevCert, prevKey := generateIPCertPEM(time.Now().Add(24*time.Hour), "192.0.2.31")
	certPath := "/etc/veil/panel/tls.crt"
	keyPath := "/etc/veil/panel/tls.key"
	sys.fileData[certPath] = prevCert
	sys.fileData[keyPath] = prevKey
	sys.files[certPath] = &fakeFileInfo{name: "tls.crt", mode: 0o640, sys: fakeSysStat(0, 995)}
	sys.files[keyPath] = &fakeFileInfo{name: "tls.key", mode: 0o640, sys: fakeSysStat(0, 995)}
	sys.chmodErrFor = map[string]error{certPath: errors.New("chmod denied")}

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "192.0.2.31", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "192.0.2.31", "--key-file", keyPath, "--fullchain-file", certPath, "--reloadcmd", renewReloadCmd(certPath, keyPath))] = commandResult{err: errors.New("installcert failed")}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "192.0.2.31", System: sys})
	if err == nil || !strings.Contains(err.Error(), "rollback") {
		t.Fatalf("rollback restore failure must propagate, got %v", err)
	}
}
