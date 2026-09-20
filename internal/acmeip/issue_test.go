package acmeip

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
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
	home           string
	homeErr        error
	uid            int
	files          map[string]*fakeFileInfo
	fileData       map[string][]byte
	commands       map[string]commandResult
	lookPaths      map[string]string
	lookPathErr    map[string]error
	portFree       map[int]bool
	mkdirErr       error
	mkdirErrFor    map[string]error
	chmodErr       error
	chmodErrFor    map[string]error
	chownCalls     []chownCall
	runCalls       [][]string
	execCalls      []string
	installAcmeSh  bool
	installSocat   bool
	installOpenSSL bool
	installCron    bool
	commandDelay   time.Duration
	events         []string
	installIPs     []string
	issuedCertPEM  []byte
	issuedKeyPEM   []byte
}

type commandResult struct {
	out        string
	err        error
	writeOwned func()
	writeFiles bool
}

type chownCall struct {
	name string
	uid  int
	gid  int
}

func newFakeSystem() *fakeSystem {
	return &fakeSystem{
		home:           "/root",
		uid:            0,
		files:          map[string]*fakeFileInfo{},
		fileData:       map[string][]byte{},
		commands:       map[string]commandResult{},
		lookPaths:      map[string]string{"curl": "/usr/bin/curl", "sh": "/bin/sh", "rm": "/bin/rm", "getent": "/usr/bin/getent", "openssl": "/usr/bin/openssl", "crontab": "/usr/bin/crontab"},
		portFree:       map[int]bool{80: true},
		installAcmeSh:  true,
		installSocat:   true,
		installOpenSSL: true,
		installCron:    true,
	}
}

func (f *fakeSystem) key(cmd string, args ...string) string {
	return fmt.Sprintf("%s %v", cmd, args)
}

func (f *fakeSystem) Run(cmd string, args ...string) error {
	f.events = append(f.events, "cleanup")
	f.runCalls = append(f.runCalls, append([]string{cmd}, args...))
	res, ok := f.commands[f.key(cmd, args...)]
	if !ok {
		return nil
	}
	return res.err
}

func (f *fakeSystem) CombinedOutputContext(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	isIssue := len(args) > 0 && args[0] == "--issue"
	if isIssue && f.commandDelay > 0 {
		timer := time.NewTimer(f.commandDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			f.events = append(f.events, "issuer-stopped")
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	out, err := f.CombinedOutput(cmd, args...)
	if isIssue {
		f.events = append(f.events, "issuer-stopped")
	}
	return out, err
}

func (f *fakeSystem) CombinedOutput(cmd string, args ...string) ([]byte, error) {
	f.execCalls = append(f.execCalls, cmd+" "+strings.Join(args, " "))
	res, ok := f.commands[f.key(cmd, args...)]
	if !ok {
		return nil, fmt.Errorf("unexpected command: %s %v", cmd, args)
	}
	if res.err == nil || res.writeFiles {
		if cmd == "sh" && len(args) >= 2 {
			script := args[1]
			if strings.Contains(script, "get.acme.sh") && f.installAcmeSh {
				acme := filepath.Join(f.home, ".acme.sh", "acme.sh")
				f.files[acme] = &fakeFileInfo{name: "acme.sh", mode: 0o755}
			}
			if strings.Contains(script, "socat") && f.installSocat {
				f.lookPaths["socat"] = "/usr/bin/socat"
			}
			if strings.Contains(script, "openssl") && f.installOpenSSL {
				f.lookPaths["openssl"] = "/usr/bin/openssl"
			}
			if strings.Contains(script, "cron") && f.installCron {
				f.lookPaths["crontab"] = "/usr/bin/crontab"
			}
		}
		if cmd == filepath.Join(f.home, ".acme.sh", "acme.sh") && len(args) >= 1 && args[0] == "--installcert" {
			certPath, keyPath, ip := parseInstallcertArgs(args)
			ips := f.installIPs
			if len(ips) == 0 {
				ips = []string{ip}
			}
			certPEM, keyPEM := f.certMaterial(ips...)
			f.fileData[certPath] = certPEM
			f.fileData[keyPath] = keyPEM
			f.files[certPath] = &fakeFileInfo{name: filepath.Base(certPath), mode: 0o600}
			f.files[keyPath] = &fakeFileInfo{name: filepath.Base(keyPath), mode: 0o600}
		}
	}
	return []byte(res.out), res.err
}

func parseInstallcertArgs(args []string) (certPath, keyPath, ip string) {
	certPath = "/etc/veil/panel/tls.crt"
	keyPath = "/etc/veil/panel/tls.key"
	for i, arg := range args {
		if i+1 >= len(args) {
			continue
		}
		switch arg {
		case "-d":
			ip = args[i+1]
		case "--fullchain-file":
			certPath = args[i+1]
		case "--key-file":
			keyPath = args[i+1]
		}
	}
	return certPath, keyPath, ip
}

func (f *fakeSystem) certMaterial(ips ...string) (certPEM, keyPEM []byte) {
	if len(f.issuedCertPEM) > 0 && len(f.issuedKeyPEM) > 0 {
		return f.issuedCertPEM, f.issuedKeyPEM
	}
	f.issuedCertPEM, f.issuedKeyPEM = generateIPCertPEM(time.Now().Add(24*time.Hour), ips...)
	return f.issuedCertPEM, f.issuedKeyPEM
}

func generateIPCertPEM(notAfter time.Time, ips ...string) (certPEM, keyPEM []byte) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "veil-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, ip := range ips {
		if parsed := net.ParseIP(ip); parsed != nil {
			template.IPAddresses = append(template.IPAddresses, parsed)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		panic(err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	return certPEM, keyPEM
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
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{out: "Installed"}

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
	sys.installIPs = []string{"1.2.3.4", "2001:db8::1"}

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	wantArgs := []string{"--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force", "-d", "2001:db8::1"}
	sys.commands[sys.key(acmeSh, wantArgs...)] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{out: "Installed"}

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
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{out: "Installed"}

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
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{out: "Installed"}

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

func TestIssueIPCertAcceptsNewCertWhenInstallcertReloadFails(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{err: errors.New("reload failed"), writeFiles: true}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err != nil {
		t.Fatalf("expected success when installcert wrote a new valid pair: %v", err)
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
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/custom/key.pem", "--fullchain-file", "/custom/cert.pem", "--reloadcmd", renewReloadCmd("/custom/cert.pem", "/custom/key.pem"))] = commandResult{out: "Installed"}

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
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{err: errors.New("reload failed")}

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
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{out: "Installed"}

	_, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureAcmeShAlreadyInstalled(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()

	acmeSh, err := ensureAcmeSh(context.Background(), sys)
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

	_, err := ensureAcmeSh(context.Background(), sys)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureAcmeShMissingCurl(t *testing.T) {
	sys := newFakeSystem()
	delete(sys.lookPaths, "curl")

	_, err := ensureAcmeSh(context.Background(), sys)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureAcmeShInstallFails(t *testing.T) {
	sys := newFakeSystem()
	sys.commands[sys.key("sh", "-c", "curl -fsSL https://get.acme.sh | sh")] = commandResult{err: errors.New("network down")}

	_, err := ensureAcmeSh(context.Background(), sys)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureAcmeShInstallSucceedsButBinaryMissing(t *testing.T) {
	sys := newFakeSystem()
	sys.installAcmeSh = false
	sys.commands[sys.key("sh", "-c", "curl -fsSL https://get.acme.sh | sh")] = commandResult{out: "installed"}

	_, err := ensureAcmeSh(context.Background(), sys)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureSocatAlreadyInstalled(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	if err := ensureSocat(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallsViaAptGet(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["apt-get"] = "/usr/bin/apt-get"
	sys.commands[sys.key("sh", "-c", "apt-get update >/dev/null 2>&1 && apt-get install -y socat")] = commandResult{out: "done"}

	if err := ensureSocat(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallsViaDnf(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["dnf"] = "/usr/bin/dnf"
	sys.commands[sys.key("sh", "-c", "dnf makecache -y >/dev/null 2>&1 && dnf -y install socat")] = commandResult{out: "done"}

	if err := ensureSocat(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallsViaYum(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["yum"] = "/usr/bin/yum"
	sys.commands[sys.key("sh", "-c", "yum makecache -y >/dev/null 2>&1 && yum -y install socat")] = commandResult{out: "done"}

	if err := ensureSocat(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallsViaPacman(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["pacman"] = "/usr/bin/pacman"
	sys.commands[sys.key("sh", "-c", "pacman -Sy --noconfirm socat")] = commandResult{out: "done"}

	if err := ensureSocat(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallsViaZypper(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["zypper"] = "/usr/bin/zypper"
	sys.commands[sys.key("sh", "-c", "zypper refresh >/dev/null 2>&1 && zypper -q install -y socat")] = commandResult{out: "done"}

	if err := ensureSocat(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallsViaApk(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["apk"] = "/sbin/apk"
	sys.commands[sys.key("sh", "-c", "apk add --no-cache socat")] = commandResult{out: "done"}

	if err := ensureSocat(context.Background(), sys); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureSocatInstallSucceedsButStillMissing(t *testing.T) {
	sys := newFakeSystem()
	sys.installSocat = false
	sys.lookPaths["apt-get"] = "/usr/bin/apt-get"
	sys.commands[sys.key("sh", "-c", "apt-get update >/dev/null 2>&1 && apt-get install -y socat")] = commandResult{out: "done"}
	// Do not add socat to lookPaths, simulating a broken install.

	if err := ensureSocat(context.Background(), sys); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureSocatInstallFails(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["apt-get"] = "/usr/bin/apt-get"
	sys.commands[sys.key("sh", "-c", "apt-get update >/dev/null 2>&1 && apt-get install -y socat")] = commandResult{err: errors.New("package not found")}

	if err := ensureSocat(context.Background(), sys); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnsureSocatNoSupportedPackageManager(t *testing.T) {
	sys := newFakeSystem()
	// Only keep unrelated binaries.
	sys.lookPaths = map[string]string{"curl": "/usr/bin/curl"}

	if err := ensureSocat(context.Background(), sys); err == nil {
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

func TestIssueIPCertDoesNotCleanupUntilCanceledIssuerStops(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"
	sys.commandDelay = 200 * time.Millisecond

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "slow"}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := IssueIPCert(ctx, IssueOptions{PublicIPv4: "1.2.3.4", System: sys})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if len(sys.events) == 0 || sys.events[0] != "issuer-stopped" {
		t.Fatalf("cleanup ran before the issuer stopped: %v", sys.events)
	}
	sawCleanup := false
	for _, event := range sys.events {
		if event == "cleanup" {
			sawCleanup = true
			break
		}
	}
	if !sawCleanup {
		t.Fatalf("expected cleanup after issuer stop, events=%v", sys.events)
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

func TestIssueIPCertCustomCAServerSkipsLEProfileAndAddsInsecure(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"
	sys.portFree[5002] = true

	caURL := "https://127.0.0.1:14000/dir"
	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", caURL)] = commandResult{out: "OK"}
	// Controlled CAs get no --certificate-profile/--days overrides, and the
	// insecure flag follows the issue call.
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", caURL, "--httpport", "5002", "--force", "--insecure")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{out: "Installed"}

	cert, err := IssueIPCert(context.Background(), IssueOptions{
		PublicIPv4: "1.2.3.4",
		HTTPPort:   5002,
		CAServer:   caURL,
		Insecure:   true,
		System:     sys,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cert.CertPath != "/etc/veil/panel/tls.crt" {
		t.Fatalf("unexpected cert path: %+v", cert)
	}
}

func TestIssueIPCertCustomCAServerWithoutInsecure(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	caURL := "https://acme-staging.example.test/dir"
	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", caURL)] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", caURL, "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{out: "Installed"}

	if _, err := IssueIPCert(context.Background(), IssueOptions{
		PublicIPv4: "1.2.3.4",
		CAServer:   caURL,
		System:     sys,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestMain stubs the group lookup so Issue paths running as root (CI
// containers) get a deterministic veil-proxy gid; individual tests that need
// real or failing lookups override lookupGroupIDFunc themselves.
func TestMain(m *testing.M) {
	orig := lookupGroupIDFunc
	lookupGroupIDFunc = func(name string) int {
		if name == "veil-proxy" {
			return 995
		}
		if name == "veil" {
			return 996
		}
		return -1
	}
	code := m.Run()
	lookupGroupIDFunc = orig
	os.Exit(code)
}

func TestFixCertOwnershipPropagatesFailures(t *testing.T) {
	// audit #528: chown/chmod errors must not be discarded — a "successful"
	// install that leaves tls.key unreadable by veil-proxy is worse than a
	// loud failure.
	sys := newFakeSystem()
	sys.files["/etc/veil/panel/tls.crt"] = &fakeFileInfo{name: "tls.crt", mode: 0o644}
	sys.files["/etc/veil/panel/tls.key"] = &fakeFileInfo{name: "tls.key", mode: 0o640}
	sys.chmodErrFor = map[string]error{"/etc/veil/panel/tls.crt": errors.New("chmod boom")}
	if err := fixCertOwnership(sys, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"); err == nil {
		t.Fatal("expected chmod error to propagate")
	}
}

func TestFixCertOwnershipAppliesProxyGroupAndDir(t *testing.T) {
	orig := getuidFunc
	getuidFunc = func() int { return 0 }
	defer func() { getuidFunc = orig }()

	sys := newFakeSystem()
	sys.files["/etc/veil/panel/tls.crt"] = &fakeFileInfo{name: "tls.crt", mode: 0o600}
	sys.files["/etc/veil/panel/tls.key"] = &fakeFileInfo{name: "tls.key", mode: 0o600}
	sys.files["/etc/veil/panel"] = &fakeFileInfo{name: "panel", mode: os.ModeDir | 0o700}

	if err := fixCertOwnership(sys, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"); err != nil {
		t.Fatalf("fixCertOwnership: %v", err)
	}
	want := map[string]int{
		"/etc/veil/panel":         995,
		"/etc/veil/panel/tls.crt": 995,
		"/etc/veil/panel/tls.key": 995,
	}
	got := map[string]int{}
	for _, c := range sys.chownCalls {
		got[c.name] = c.gid
		if c.uid != 0 {
			t.Fatalf("chown %s used uid %d, want 0", c.name, c.uid)
		}
	}
	for path, gid := range want {
		if got[path] != gid {
			t.Fatalf("chownCalls[%s] gid=%d, want %d (all: %+v)", path, got[path], gid, sys.chownCalls)
		}
	}
	if sys.files["/etc/veil/panel"].mode.Perm() != 0o750 {
		t.Fatalf("panel dir mode = %o, want 0750", sys.files["/etc/veil/panel"].mode.Perm())
	}
}

func TestFixCertOwnershipFailsWhenNoRuntimeGroup(t *testing.T) {
	orig := getuidFunc
	getuidFunc = func() int { return 0 }
	defer func() { getuidFunc = orig }()
	origLookup := lookupGroupIDFunc
	lookupGroupIDFunc = func(string) int { return -1 }
	defer func() { lookupGroupIDFunc = origLookup }()

	sys := newFakeSystem()
	sys.files["/etc/veil/panel/tls.crt"] = &fakeFileInfo{name: "tls.crt", mode: 0o644}
	sys.files["/etc/veil/panel/tls.key"] = &fakeFileInfo{name: "tls.key", mode: 0o640}
	if err := fixCertOwnership(sys, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"); err == nil {
		t.Fatal("expected error when neither veil-proxy nor veil group resolves")
	}
}
