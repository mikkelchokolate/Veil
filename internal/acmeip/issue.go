package acmeip

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"time"
)

// IssuedCert holds the on-disk paths of a successfully issued certificate.
type IssuedCert struct {
	CertPath string
	KeyPath  string
}

// IssueOptions configures a Let's Encrypt IP-certificate request.
type IssueOptions struct {
	PublicIPv4 string
	PublicIPv6 string
	HTTPPort   int
	Email      string
	CertPath   string
	KeyPath    string
	System     System
}

// System abstracts command execution and file operations so IssueIPCert can be
// unit-tested without root privileges or network access.
type System interface {
	Run(cmd string, args ...string) error
	CombinedOutput(cmd string, args ...string) ([]byte, error)
	LookPath(name string) (string, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Chmod(name string, perm os.FileMode) error
	Chown(name string, uid, gid int) error
	Stat(name string) (os.FileInfo, error)
	HomeDir() (string, error)
	IsPortFree(port int) bool
}

// defaultSystem implements System using the real host.
type defaultSystem struct{}

func (defaultSystem) Run(cmd string, args ...string) error {
	return exec.Command(cmd, args...).Run()
}

func (defaultSystem) CombinedOutput(cmd string, args ...string) ([]byte, error) {
	return exec.Command(cmd, args...).CombinedOutput()
}

func (defaultSystem) LookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func (defaultSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (defaultSystem) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}

func (defaultSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (defaultSystem) Chmod(name string, perm os.FileMode) error {
	return os.Chmod(name, perm)
}

func (defaultSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// userHomeDirFunc allows tests to mock the os.UserHomeDir fallback.
var userHomeDirFunc = os.UserHomeDir

func (defaultSystem) HomeDir() (string, error) {
	if home := os.Getenv("HOME"); home != "" {
		return home, nil
	}
	return userHomeDirFunc()
}

func (defaultSystem) IsPortFree(port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func defaultSystemOr(s System) System {
	if s != nil {
		return s
	}
	return defaultSystem{}
}

// IssueIPCert obtains a Let's Encrypt shortlived certificate for the given IP
// address(es) using acme.sh standalone mode. It installs acme.sh and socat if
// they are missing, writes the certificate material to CertPath/KeyPath, and
// registers an acme.sh reloadcmd that restarts veil on renewal.
//
// Port 80 (or HTTPPort) must be free and reachable from the internet.
func IssueIPCert(ctx context.Context, opts IssueOptions) (IssuedCert, error) {
	sys := defaultSystemOr(opts.System)

	if opts.PublicIPv4 == "" {
		return IssuedCert{}, fmt.Errorf("public IPv4 address is required")
	}
	if net.ParseIP(opts.PublicIPv4) == nil {
		return IssuedCert{}, fmt.Errorf("public IPv4 %q is not a valid IP address", opts.PublicIPv4)
	}
	if opts.PublicIPv6 != "" && net.ParseIP(opts.PublicIPv6) == nil {
		return IssuedCert{}, fmt.Errorf("public IPv6 %q is not a valid IP address", opts.PublicIPv6)
	}

	certPath := opts.CertPath
	if certPath == "" {
		certPath = "/etc/veil/panel/tls.crt"
	}
	keyPath := opts.KeyPath
	if keyPath == "" {
		keyPath = "/etc/veil/panel/tls.key"
	}
	httpPort := opts.HTTPPort
	if httpPort <= 0 {
		httpPort = 80
	}

	acmeSh, err := ensureAcmeSh(sys)
	if err != nil {
		return IssuedCert{}, fmt.Errorf("acme.sh setup: %w", err)
	}
	if err := ensureSocat(sys); err != nil {
		return IssuedCert{}, fmt.Errorf("socat setup: %w", err)
	}
	if !sys.IsPortFree(httpPort) {
		return IssuedCert{}, fmt.Errorf("port %d is already in use; Let's Encrypt HTTP-01 validation needs a free port %d (forward external port 80 if you use a non-standard port)", httpPort, httpPort)
	}

	if err := ensureCertDirs(sys, certPath, keyPath); err != nil {
		return IssuedCert{}, err
	}

	// Set default CA so the first issue does not hit ZeroSSL.
	if out, err := sys.CombinedOutput(acmeSh, "--set-default-ca", "--server", "letsencrypt"); err != nil {
		return IssuedCert{}, fmt.Errorf("set default CA: %w (output: %s)", err, string(out))
	}

	issueArgs := []string{
		"--issue",
		"-d", opts.PublicIPv4,
		"--standalone",
		"--server", "letsencrypt",
		"--certificate-profile", "shortlived",
		"--days", "3",
		"--httpport", strconv.Itoa(httpPort),
		"--force",
	}
	if opts.PublicIPv6 != "" {
		issueArgs = append(issueArgs, "-d", opts.PublicIPv6)
	}

	issueCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if out, err := runWithContext(issueCtx, sys, acmeSh, issueArgs...); err != nil {
		cleanupAcmeState(sys, acmeSh, opts.PublicIPv4, opts.PublicIPv6)
		return IssuedCert{}, fmt.Errorf("issue certificate for %s: %w (output: %s)", opts.PublicIPv4, err, string(out))
	}

	installArgs := []string{
		"--installcert",
		"-d", opts.PublicIPv4,
		"--key-file", keyPath,
		"--fullchain-file", certPath,
		"--reloadcmd", "systemctl restart veil || true",
	}
	if out, err := sys.CombinedOutput(acmeSh, installArgs...); err != nil {
		// acme.sh returns non-zero when the reloadcmd fails, but the files may
		// still have been installed. Verify before failing.
		if _, certErr := sys.Stat(certPath); certErr != nil {
			cleanupAcmeState(sys, acmeSh, opts.PublicIPv4, opts.PublicIPv6)
			return IssuedCert{}, fmt.Errorf("install certificate: %w (output: %s)", err, string(out))
		}
	}

	if err := fixCertOwnership(sys, certPath, keyPath); err != nil {
		return IssuedCert{}, fmt.Errorf("fix certificate ownership: %w", err)
	}

	return IssuedCert{CertPath: certPath, KeyPath: keyPath}, nil
}

func ensureAcmeSh(sys System) (string, error) {
	home, err := sys.HomeDir()
	if err != nil {
		return "", fmt.Errorf("home directory: %w", err)
	}
	acmeSh := filepath.Join(home, ".acme.sh", "acme.sh")
	if _, err := sys.Stat(acmeSh); err == nil {
		return acmeSh, nil
	}

	if _, err := sys.LookPath("curl"); err != nil {
		return "", fmt.Errorf("curl is required to install acme.sh")
	}

	args := []string{"-c", "curl -fsSL https://get.acme.sh | sh"}
	if out, err := sys.CombinedOutput("sh", args...); err != nil {
		return "", fmt.Errorf("install acme.sh: %w (output: %s)", err, string(out))
	}

	if _, err := sys.Stat(acmeSh); err != nil {
		return "", fmt.Errorf("acme.sh did not appear at %s after install", acmeSh)
	}
	return acmeSh, nil
}

func ensureSocat(sys System) error {
	if _, err := sys.LookPath("socat"); err == nil {
		return nil
	}

	managers := []struct {
		name string
		cmd  string
		args []string
	}{
		{"apt-get", "sh", []string{"-c", "apt-get update >/dev/null 2>&1 && apt-get install -y socat"}},
		{"dnf", "sh", []string{"-c", "dnf makecache -y >/dev/null 2>&1 && dnf -y install socat"}},
		{"yum", "sh", []string{"-c", "yum makecache -y >/dev/null 2>&1 && yum -y install socat"}},
		{"pacman", "sh", []string{"-c", "pacman -Sy --noconfirm socat"}},
		{"zypper", "sh", []string{"-c", "zypper refresh >/dev/null 2>&1 && zypper -q install -y socat"}},
		{"apk", "sh", []string{"-c", "apk add --no-cache socat"}},
	}

	for _, m := range managers {
		if _, err := sys.LookPath(m.name); err != nil {
			continue
		}
		if out, err := sys.CombinedOutput(m.cmd, m.args...); err != nil {
			return fmt.Errorf("install socat via %s: %w (output: %s)", m.name, err, string(out))
		}
		if _, err := sys.LookPath("socat"); err == nil {
			return nil
		}
		return fmt.Errorf("socat installation via %s completed but socat is not in PATH", m.name)
	}
	return fmt.Errorf("no supported package manager found to install socat")
}

func ensureCertDirs(sys System, certPath, keyPath string) error {
	certDir := filepath.Dir(certPath)
	keyDir := filepath.Dir(keyPath)
	if certDir != "" {
		if err := sys.MkdirAll(certDir, 0o750); err != nil {
			return fmt.Errorf("create cert directory %s: %w", certDir, err)
		}
	}
	if keyDir != "" && keyDir != certDir {
		if err := sys.MkdirAll(keyDir, 0o750); err != nil {
			return fmt.Errorf("create key directory %s: %w", keyDir, err)
		}
	}
	return nil
}

// getuidFunc allows tests to mock the current UID without changing the real
// process owner.
var getuidFunc = os.Getuid

// chownFunc allows tests to mock file ownership changes.
var chownFunc = os.Chown

func (defaultSystem) Chown(name string, uid, gid int) error {
	return chownFunc(name, uid, gid)
}

func fixCertOwnership(sys System, certPath, keyPath string) error {
	if err := sys.Chmod(certPath, 0o644); err != nil {
		return err
	}
	if err := sys.Chmod(keyPath, 0o640); err != nil {
		return err
	}
	// The panel service runs as the veil user. If we are root and the veil
	// group exists, make the key readable by that group.
	if uid := getuidFunc(); uid == 0 {
		if gid := lookupGroupID("veil"); gid >= 0 {
			_ = sys.Chown(certPath, 0, gid)
			_ = sys.Chown(keyPath, 0, gid)
		}
	}
	return nil
}

func lookupGroupID(name string) int {
	if g, err := user.LookupGroup(name); err == nil {
		if gid, err := strconv.Atoi(g.Gid); err == nil {
			return gid
		}
	}
	return -1
}

func cleanupAcmeState(sys System, acmeSh, ipv4, ipv6 string) {
	home, _ := sys.HomeDir()
	for _, ip := range []string{ipv4, ipv6} {
		if ip == "" {
			continue
		}
		_ = sys.Run("rm", "-rf", filepath.Join(home, ".acme.sh", ip), filepath.Join(home, ".acme.sh", ip+"_ecc"))
		if acmeSh != "" {
			_ = sys.Run(acmeSh, "--remove", "-d", ip)
		}
	}
}

func runWithContext(ctx context.Context, sys System, cmd string, args ...string) ([]byte, error) {
	outCh := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		out, err := sys.CombinedOutput(cmd, args...)
		outCh <- out
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case out := <-outCh:
		return out, <-errCh
	}
}
