package acmeip

import (
	"context"
	"errors"
	"fmt"
	"net"
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
	home          string
	homeErr       error
	uid           int
	files         map[string]*fakeFileInfo
	fileData      map[string][]byte
	commands      map[string]commandResult
	lookPaths     map[string]string
	lookPathErr   map[string]error
	portFree      map[int]bool
	mkdirErr      error
	mkdirErrFor   map[string]error
	chmodErr      error
	chmodErrFor   map[string]error
	chownCalls    []chownCall
	runCalls      [][]string
	installAcmeSh bool
	installSocat  bool
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
		home:          "/root",
		uid:           0,
		files:         map[string]*fakeFileInfo{},
		fileData:      map[string][]byte{},
		commands:      map[string]commandResult{},
		lookPaths:     map[string]string{"curl": "/usr/bin/curl", "sh": "/bin/sh", "rm": "/bin/rm", "getent": "/usr/bin/getent"},
		portFree:      map[int]bool{80: true},
		installAcmeSh: true,
		installSocat:  true,
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
	res, ok := f.commands[f.key(cmd, args...)]
	if !ok {
		return nil, fmt.Errorf("unexpected command: %s %v", cmd, args)
	}
	// Only simulate side effects when the mocked command succeeds.
	if res.err == nil {
		if cmd == "sh" && len(args) >= 2 {
			script := args[1]
			if strings.Contains(script, "get.acme.sh") && f.installAcmeSh {
				acme := filepath.Join(f.home, ".acme.sh", "acme.sh")
				f.files[acme] = &fakeFileInfo{name: "acme.sh", mode: 0o755}
			}
			if strings.Contains(script, "socat") && f.installSocat {
				f.lookPaths["socat"] = "/usr/bin/socat"
			}
		}
		if cmd == filepath.Join(f.home, ".acme.sh", "acme.sh") && len(args) >= 1 && args[0] == "--installcert" {
			f.files["/etc/veil/panel/tls.crt"] = &fakeFileInfo{name: "tls.crt", mode: 0o600}
			f.files["/etc/veil/panel/tls.key"] = &fakeFileInfo{name: "tls.key", mode: 0o600}
		}
	}
	return []byte(res.out), res.err
}

func (f *fakeSystem) LookPath(name string) (string, error) {
	if err, ok := f.lookPathErr[name]; ok {
		return "", err
	}
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
	if err, ok := f.mkdirErrFor[path]; ok {
		return err
	}
	if f.mkdirErr != nil {
		return f.mkdirErr
	}
	f.files[path] = &fakeFileInfo{name: filepath.Base(path), mode: perm | os.ModeDir}
	return nil
}

func (f *fakeSystem) Chmod(name string, perm os.FileMode) error {
	if err, ok := f.chmodErrFor[name]; ok {
		return err
	}
	if f.chmodErr != nil {
		return f.chmodErr
	}
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
	return f.home, f.homeErr
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

func TestIssueIPCertCustomPathsAndPort(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"
	sys.portFree[8080] = true

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "8080", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/custom/key.pem", "--fullchain-file", "/custom/cert.pem", "--reloadcmd", "systemctl restart veil || true")] = commandResult{out: "Installed"}

	cert, err := IssueIPCert(context.Background(), IssueOptions{
		PublicIPv4: "1.2.3.4",
		HTTPPort:   8080,
		CertPath:   "/custom/cert.pem",
		KeyPath:    "/custom/key.pem",
		System:     sys,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert.CertPath != "/custom/cert.pem" || cert.KeyPath != "/custom/key.pem" {
		t.Fatalf("unexpected cert paths: %+v", cert)
	}
}

func TestIssueIPCertSetDefaultCAFails(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{err: errors.New("CA unreachable")}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestIssueIPCertEnsureAcmeShFails(t *testing.T) {
	sys := newFakeSystem()
	sys.homeErr = errors.New("no home")

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestIssueIPCertEnsureSocatFails(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestIssueIPCertEnsureCertDirsFails(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"
	sys.mkdirErr = errors.New("permission denied")

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestIssueIPCertInstallcertFailsAndFilesMissing(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", "systemctl restart veil || true")] = commandResult{err: errors.New("reload failed")}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestIssueIPCertFixOwnershipFails(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"
	sys.chmodErr = errors.New("permission denied")

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", "systemctl restart veil || true")] = commandResult{out: "Installed"}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureAcmeShAlreadyInstalled(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()

	acmeSh, err := ensureAcmeSh(sys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if acmeSh != filepath.Join(sys.home, ".acme.sh", "acme.sh") {
		t.Fatalf("unexpected acme.sh path: %s", acmeSh)
	}
}

func TestEnsureAcmeShHomeDirError(t *testing.T) {
	sys := newFakeSystem()
	sys.homeErr = errors.New("no home")

	_, err := ensureAcmeSh(sys)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureAcmeShMissingCurl(t *testing.T) {
	sys := newFakeSystem()
	delete(sys.lookPaths, "curl")

	_, err := ensureAcmeSh(sys)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureAcmeShInstallFails(t *testing.T) {
	sys := newFakeSystem()
	sys.commands[sys.key("sh", "-c", "curl -fsSL https://get.acme.sh | sh")] = commandResult{err: errors.New("network down")}

	_, err := ensureAcmeSh(sys)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureAcmeShInstallSucceedsButBinaryMissing(t *testing.T) {
	sys := newFakeSystem()
	sys.installAcmeSh = false
	sys.commands[sys.key("sh", "-c", "curl -fsSL https://get.acme.sh | sh")] = commandResult{out: "installed"}

	_, err := ensureAcmeSh(sys)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureSocatAlreadyInstalled(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	if err := ensureSocat(sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallsViaAptGet(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["apt-get"] = "/usr/bin/apt-get"
	sys.commands[sys.key("sh", "-c", "apt-get update >/dev/null 2>&1 && apt-get install -y socat")] = commandResult{out: "done"}

	if err := ensureSocat(sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallsViaDnf(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["dnf"] = "/usr/bin/dnf"
	sys.commands[sys.key("sh", "-c", "dnf makecache -y >/dev/null 2>&1 && dnf -y install socat")] = commandResult{out: "done"}

	if err := ensureSocat(sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallsViaYum(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["yum"] = "/usr/bin/yum"
	sys.commands[sys.key("sh", "-c", "yum makecache -y >/dev/null 2>&1 && yum -y install socat")] = commandResult{out: "done"}

	if err := ensureSocat(sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallsViaPacman(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["pacman"] = "/usr/bin/pacman"
	sys.commands[sys.key("sh", "-c", "pacman -Sy --noconfirm socat")] = commandResult{out: "done"}

	if err := ensureSocat(sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallsViaZypper(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["zypper"] = "/usr/bin/zypper"
	sys.commands[sys.key("sh", "-c", "zypper refresh >/dev/null 2>&1 && zypper -q install -y socat")] = commandResult{out: "done"}

	if err := ensureSocat(sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallsViaApk(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["apk"] = "/sbin/apk"
	sys.commands[sys.key("sh", "-c", "apk add --no-cache socat")] = commandResult{out: "done"}

	if err := ensureSocat(sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallSucceedsButStillMissing(t *testing.T) {
	sys := newFakeSystem()
	sys.installSocat = false
	sys.lookPaths["apt-get"] = "/usr/bin/apt-get"
	sys.commands[sys.key("sh", "-c", "apt-get update >/dev/null 2>&1 && apt-get install -y socat")] = commandResult{out: "done"}
	// Do not add socat to lookPaths, simulating a broken install.

	if err := ensureSocat(sys); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureSocatInstallFails(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["apt-get"] = "/usr/bin/apt-get"
	sys.commands[sys.key("sh", "-c", "apt-get update >/dev/null 2>&1 && apt-get install -y socat")] = commandResult{err: errors.New("package not found")}

	if err := ensureSocat(sys); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureSocatNoSupportedPackageManager(t *testing.T) {
	sys := newFakeSystem()
	// Only keep unrelated binaries.
	sys.lookPaths = map[string]string{"curl": "/usr/bin/curl"}

	if err := ensureSocat(sys); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureCertDirsSameDirectory(t *testing.T) {
	sys := newFakeSystem()

	if err := ensureCertDirs(sys, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := sys.files["/etc/veil/panel"]; !ok {
		t.Fatal("expected cert directory to be created")
	}
}

func TestEnsureCertDirsDifferentDirectories(t *testing.T) {
	sys := newFakeSystem()

	if err := ensureCertDirs(sys, "/etc/veil/panel/tls.crt", "/etc/veil/secrets/tls.key"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := sys.files["/etc/veil/panel"]; !ok {
		t.Fatal("expected cert directory to be created")
	}
	if _, ok := sys.files["/etc/veil/secrets"]; !ok {
		t.Fatal("expected key directory to be created")
	}
}

func TestEnsureCertDirsMkdirAllFails(t *testing.T) {
	sys := newFakeSystem()
	sys.mkdirErr = errors.New("permission denied")

	if err := ensureCertDirs(sys, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"); err == nil {
		t.Fatal("expected error")
	}
}

func TestFixCertOwnershipChmodCertFails(t *testing.T) {
	sys := newFakeSystem()
	sys.files["/etc/veil/panel/tls.crt"] = &fakeFileInfo{name: "tls.crt", mode: 0o600}
	sys.files["/etc/veil/panel/tls.key"] = &fakeFileInfo{name: "tls.key", mode: 0o600}
	sys.chmodErr = errors.New("chmod failed")

	if err := fixCertOwnership(sys, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"); err == nil {
		t.Fatal("expected error")
	}
}

func TestFixCertOwnershipChmodKeyFails(t *testing.T) {
	sys := newFakeSystem()
	sys.files["/etc/veil/panel/tls.crt"] = &fakeFileInfo{name: "tls.crt", mode: 0o600}
	sys.files["/etc/veil/panel/tls.key"] = &fakeFileInfo{name: "tls.key", mode: 0o600}
	sys.chmodErrFor = map[string]error{
		"/etc/veil/panel/tls.key": errors.New("chmod key failed"),
	}

	if err := fixCertOwnership(sys, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"); err == nil {
		t.Fatal("expected error")
	}
}

func TestFixCertOwnershipNonRootSkipsChown(t *testing.T) {
	orig := getuidFunc
	getuidFunc = func() int { return 1000 }
	defer func() { getuidFunc = orig }()

	sys := newFakeSystem()
	sys.files["/etc/veil/panel/tls.crt"] = &fakeFileInfo{name: "tls.crt", mode: 0o600}
	sys.files["/etc/veil/panel/tls.key"] = &fakeFileInfo{name: "tls.key", mode: 0o600}

	if err := fixCertOwnership(sys, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sys.chownCalls) != 0 {
		t.Fatalf("expected no chown calls, got %v", sys.chownCalls)
	}
}

func TestRunWithContextCancellation(t *testing.T) {
	sys := newFakeSystem()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := runWithContext(ctx, sys, "sleep", "10")
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestLookupGroupIDExisting(t *testing.T) {
	if gid := lookupGroupID("root"); gid < 0 {
		t.Fatalf("expected root group to exist, got %d", gid)
	}
}

func TestLookupGroupIDNonExisting(t *testing.T) {
	if gid := lookupGroupID("definitely-not-a-real-group-name"); gid != -1 {
		t.Fatalf("expected -1 for non-existing group, got %d", gid)
	}
}

func TestDefaultSystemOrReturnsDefaultWhenNil(t *testing.T) {
	sys := defaultSystemOr(nil)
	if sys == nil {
		t.Fatal("expected non-nil System")
	}
	if _, ok := sys.(defaultSystem); !ok {
		t.Fatalf("expected defaultSystem, got %T", sys)
	}
}

func TestDefaultSystemOrReturnsProvided(t *testing.T) {
	fake := newFakeSystem()
	if sys := defaultSystemOr(fake); sys != fake {
		t.Fatal("expected provided system to be returned")
	}
}

func TestDefaultSystemIsPortFree(t *testing.T) {
	sys := defaultSystem{}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	if !sys.IsPortFree(port) {
		t.Fatalf("expected port %d to be free", port)
	}

	ln2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln2.Close()

	if sys.IsPortFree(port) {
		t.Fatalf("expected port %d to be busy", port)
	}
}

func TestDefaultSystemHomeDir(t *testing.T) {
	sys := defaultSystem{}
	home, err := sys.HomeDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if home == "" {
		t.Fatal("expected non-empty home directory")
	}
}

func TestDefaultSystemLookPath(t *testing.T) {
	sys := defaultSystem{}
	if _, err := sys.LookPath("sh"); err != nil {
		t.Fatalf("expected sh in PATH: %v", err)
	}
}

func TestDefaultSystemStat(t *testing.T) {
	sys := defaultSystem{}
	path := "issue_test.go"
	fi, err := sys.Stat(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fi.Name() != path {
		t.Fatalf("expected name %q, got %q", path, fi.Name())
	}
}

func TestDefaultSystemReadFile(t *testing.T) {
	sys := defaultSystem{}
	data, err := sys.ReadFile("issue_test.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty file")
	}
}

func TestDefaultSystemWriteFileMkdirAllChmod(t *testing.T) {
	sys := defaultSystem{}
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "file.txt")

	if err := sys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := sys.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := sys.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	data, err := sys.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestDefaultSystemCombinedOutput(t *testing.T) {
	sys := defaultSystem{}
	out, err := sys.CombinedOutput("go", "version")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "go version") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestDefaultSystemRun(t *testing.T) {
	sys := defaultSystem{}
	if err := sys.Run("true"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultSystemHomeDirFallsBackWithoutEnv(t *testing.T) {
	orig := userHomeDirFunc
	userHomeDirFunc = func() (string, error) { return "/fallback/home", nil }
	defer func() { userHomeDirFunc = orig }()

	t.Setenv("HOME", "")
	sys := defaultSystem{}
	home, err := sys.HomeDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if home != "/fallback/home" {
		t.Fatalf("unexpected home: %s", home)
	}
}

func TestDefaultSystemChown(t *testing.T) {
	orig := chownFunc
	called := false
	chownFunc = func(name string, uid, gid int) error {
		called = true
		return nil
	}
	defer func() { chownFunc = orig }()

	sys := defaultSystem{}
	if err := sys.Chown("/some/path", 0, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected chownFunc to be called")
	}
}

func TestEnsureCertDirsKeyDirMkdirAllFails(t *testing.T) {
	sys := newFakeSystem()
	sys.mkdirErrFor = map[string]error{
		"/etc/veil/secrets": errors.New("permission denied"),
	}

	if err := ensureCertDirs(sys, "/etc/veil/panel/tls.crt", "/etc/veil/secrets/tls.key"); err == nil {
		t.Fatal("expected error")
	}
}
