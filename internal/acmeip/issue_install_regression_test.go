package acmeip

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
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
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "192.0.2.20", "--key-file", keyPath, "--fullchain-file", certPath, "--reloadcmd", "systemctl restart veil || true")] = commandResult{err: errors.New("installcert failed")}

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
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "192.0.2.20", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", "systemctl restart veil || true")] = commandResult{err: errors.New("installcert failed")}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "192.0.2.20", System: sys})
	if err == nil {
		t.Fatal("expected error when installcert failed and no certificate was written")
	}
}
