package acmeip

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeFileInfo struct {
	name string
	mode os.FileMode
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }

type fakeSystem struct {
	home       string
	uid        int
	files      map[string]*fakeFileInfo
	fileData   map[string][]byte
	commands   map[string]commandResult
	lookPaths  map[string]string
	portFree   map[int]bool
	chownCalls []chownCall
	runCalls   [][]string
}

type commandResult struct {
	out string
	err error
}

type chownCall struct {
	name string
	uid  int
	gid  int
}

func newFakeSystem() *fakeSystem {
	return &fakeSystem{
		home:      "/root",
		uid:       0,
		files:     map[string]*fakeFileInfo{},
		fileData:  map[string][]byte{},
		commands:  map[string]commandResult{},
		lookPaths: map[string]string{"curl": "/usr/bin/curl", "sh": "/bin/sh", "rm": "/bin/rm", "getent": "/usr/bin/getent"},
		portFree:  map[int]bool{80: true},
	}
}

func (f *fakeSystem) key(cmd string, args ...string) string {
	return fmt.Sprintf("%s %v", cmd, args)
}

func (f *fakeSystem) Run(cmd string, args ...string) error {
	f.runCalls = append(f.runCalls, append([]string{cmd}, args...))
	res, ok := f.commands[f.key(cmd, args...)]
	if !ok {
		return nil
	}
	return res.err
}

func (f *fakeSystem) CombinedOutput(cmd string, args ...string) ([]byte, error) {
	if cmd == "sh" && len(args) >= 2 {
		script := args[1]
		if strings.Contains(script, "get.acme.sh") {
			acme := filepath.Join(f.home, ".acme.sh", "acme.sh")
			f.files[acme] = &fakeFileInfo{name: "acme.sh", mode: 0o755}
		}
		if strings.Contains(script, "socat") && strings.Contains(script, "apt-get") {
			f.lookPaths["socat"] = "/usr/bin/socat"
		}
	}
	if cmd == filepath.Join(f.home, ".acme.sh", "acme.sh") && len(args) >= 1 && args[0] == "--installcert" {
		f.files["/etc/veil/panel/tls.crt"] = &fakeFileInfo{name: "tls.crt", mode: 0o600}
		f.files["/etc/veil/panel/tls.key"] = &fakeFileInfo{name: "tls.key", mode: 0o600}
	}
	res, ok := f.commands[f.key(cmd, args...)]
	if !ok {
		return nil, fmt.Errorf("unexpected command: %s %v", cmd, args)
	}
	return []byte(res.out), res.err
}

func (f *fakeSystem) LookPath(name string) (string, error) {
	if p, ok := f.lookPaths[name]; ok {
		return p, nil
	}
	return "", errors.New("not found")
}

func (f *fakeSystem) ReadFile(name string) ([]byte, error) {
	if d, ok := f.fileData[name]; ok {
		return d, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	f.fileData[name] = data
	f.files[name] = &fakeFileInfo{name: filepath.Base(name), mode: perm &^ os.ModeDir}
	return nil
}

func (f *fakeSystem) MkdirAll(path string, perm os.FileMode) error {
	f.files[path] = &fakeFileInfo{name: filepath.Base(path), mode: perm | os.ModeDir}
	return nil
}

func (f *fakeSystem) Chmod(name string, perm os.FileMode) error {
	if fi, ok := f.files[name]; ok {
		fi.mode = perm
	}
	return nil
}

func (f *fakeSystem) Chown(name string, uid, gid int) error {
	f.chownCalls = append(f.chownCalls, chownCall{name: name, uid: uid, gid: gid})
	return nil
}

func (f *fakeSystem) Stat(name string) (os.FileInfo, error) {
	if fi, ok := f.files[name]; ok {
		return fi, nil
	}
	return nil, os.ErrNotExist
}

func (f *fakeSystem) HomeDir() (string, error) {
	return f.home, nil
}

func (f *fakeSystem) IsPortFree(port int) bool {
	return f.portFree[port]
}

func (f *fakeSystem) setAcmeInstalled() {
	acme := filepath.Join(f.home, ".acme.sh", "acme.sh")
	f.files[acme] = &fakeFileInfo{name: "acme.sh", mode: 0o755}
}

func TestIssueIPCertRequiresPublicIPv4(t *testing.T) {
	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "", System: newFakeSystem()})
	if err == nil {
		t.Fatal("expected error for missing IPv4")
	}
}

func TestIssueIPCertRejectsInvalidIPv4(t *testing.T) {
	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "not-an-ip", System: newFakeSystem()})
	if err == nil {
		t.Fatal("expected error for invalid IPv4")
	}
}

func TestIssueIPCertRejectsInvalidIPv6(t *testing.T) {
	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", PublicIPv6: "bad", System: newFakeSystem()})
	if err == nil {
		t.Fatal("expected error for invalid IPv6")
	}
}

func TestIssueIPCertRejectsBusyPort(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"
	sys.portFree[80] = false
	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err == nil {
		t.Fatal("expected error when port 80 is busy")
	}
}

func TestIssueIPCertIssuesAndInstallsCertificate(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", "systemctl restart veil || true")] = commandResult{out: "Installed"}

	cert, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert.CertPath != "/etc/veil/panel/tls.crt" || cert.KeyPath != "/etc/veil/panel/tls.key" {
		t.Fatalf("unexpected cert paths: %+v", cert)
	}
	if sys.files[cert.CertPath].mode != 0o644 {
		t.Fatalf("cert mode = %o, want 0644", sys.files[cert.CertPath].mode)
	}
	if sys.files[cert.KeyPath].mode != 0o640 {
		t.Fatalf("key mode = %o, want 0640", sys.files[cert.KeyPath].mode)
	}
}

func TestIssueIPCertIncludesIPv6WhenProvided(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	wantArgs := []string{"--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force", "-d", "2001:db8::1"}
	sys.commands[sys.key(acmeSh, wantArgs...)] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", "systemctl restart veil || true")] = commandResult{out: "Installed"}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", PublicIPv6: "2001:db8::1", System: sys})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIssueIPCertInstallsAcmeShWhenMissing(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key("sh", "-c", "curl -fsSL https://get.acme.sh | sh")] = commandResult{out: "installed"}
	// After install, acme.sh is present.
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", "systemctl restart veil || true")] = commandResult{out: "Installed"}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := sys.files[acmeSh]; !ok {
		t.Fatal("expected acme.sh to be recorded as installed")
	}
}

func TestIssueIPCertInstallsSocatWhenMissing(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", "systemctl restart veil || true")] = commandResult{out: "Installed"}

	// socat not in lookPaths, but apt-get is.
	sys.lookPaths["apt-get"] = "/usr/bin/apt-get"
	sys.commands[sys.key("sh", "-c", "apt-get update >/dev/null 2>&1 && apt-get install -y socat")] = commandResult{out: "done"}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIssueIPCertCleansUpOnIssueFailure(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{err: errors.New("validation failed")}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err == nil {
		t.Fatal("expected error")
	}

	// The cleanup removes acme.sh state directories and runs --remove.
	want := []string{"rm", "-rf", filepath.Join(sys.home, ".acme.sh", "1.2.3.4"), filepath.Join(sys.home, ".acme.sh", "1.2.3.4_ecc")}
	found := false
	for _, call := range sys.runCalls {
		if reflect.DeepEqual(call, want) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("cleanup rm not called; calls: %v", sys.runCalls)
	}
}

func TestIssueIPCertSucceedsWhenInstallcertFailsButFilesExist(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", "systemctl restart veil || true")] = commandResult{err: errors.New("reload failed")}

	// Simulate that acme.sh still wrote the files.
	sys.files["/etc/veil/panel/tls.crt"] = &fakeFileInfo{name: "tls.crt", mode: 0o600}
	sys.files["/etc/veil/panel/tls.key"] = &fakeFileInfo{name: "tls.key", mode: 0o600}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err != nil {
		t.Fatalf("expected success when files exist despite installcert error: %v", err)
	}
}
