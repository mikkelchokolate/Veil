package acmeip

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
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
	// CAServer overrides the acme.sh --server value (an acme.sh CA name like
	// "letsencrypt" or a directory URL such as a controlled test CA). Empty
	// means Let's Encrypt. Insecure adds acme.sh --insecure, which skips TLS
	// verification of the ACME endpoint — for controlled test CAs only.
	CAServer string
	Insecure bool
}

// System abstracts command execution and file operations so IssueIPCert can be
// unit-tested without root privileges or network access.
type System interface {
	Run(cmd string, args ...string) error
	CombinedOutput(cmd string, args ...string) ([]byte, error)
	CombinedOutputContext(ctx context.Context, cmd string, args ...string) ([]byte, error)
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

func (defaultSystem) CombinedOutputContext(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	command := exec.Command(cmd, args...)
	configureProcessGroup(command)
	var buf bytes.Buffer
	command.Stdout = &buf
	command.Stderr = &buf
	if err := command.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case <-ctx.Done():
		killProcessGroup(command)
		<-done
		return buf.Bytes(), ctx.Err()
	case err := <-done:
		return buf.Bytes(), err
	}
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

	if err := ensureAcmePrereqs(ctx, sys); err != nil {
		return IssuedCert{}, fmt.Errorf("acme prerequisites: %w", err)
	}
	acmeSh, err := ensureAcmeSh(ctx, sys)
	if err != nil {
		return IssuedCert{}, fmt.Errorf("acme.sh setup: %w", err)
	}
	if !sys.IsPortFree(httpPort) {
		return IssuedCert{}, fmt.Errorf("port %d is already in use; Let's Encrypt HTTP-01 validation needs a free port %d (forward external port 80 if you use a non-standard port)", httpPort, httpPort)
	}

	if err := ensureCertDirs(sys, certPath, keyPath); err != nil {
		return IssuedCert{}, err
	}

	caServer := strings.TrimSpace(opts.CAServer)
	if caServer == "" {
		caServer = "letsencrypt"
	}
	// Set default CA so the first issue does not hit ZeroSSL.
	if out, err := runWithContext(ctx, sys, acmeSh, "--set-default-ca", "--server", caServer); err != nil {
		return IssuedCert{}, fmt.Errorf("set default CA: %w (output: %s)", err, string(out))
	}

	issueArgs := []string{
		"--issue",
		"-d", opts.PublicIPv4,
		"--standalone",
		"--server", caServer,
	}
	if caServer == "letsencrypt" {
		// Let's Encrypt's shortlived profile is required for IP certificates;
		// other CAs (e.g. a controlled test CA) reject profile/days overrides.
		issueArgs = append(issueArgs, "--certificate-profile", "shortlived", "--days", "3")
	}
	issueArgs = append(issueArgs, "--httpport", strconv.Itoa(httpPort), "--force")
	if opts.Insecure {
		issueArgs = append(issueArgs, "--insecure")
	}
	if opts.PublicIPv6 != "" {
		issueArgs = append(issueArgs, "-d", opts.PublicIPv6)
	}

	home, err := sys.HomeDir()
	if err != nil {
		return IssuedCert{}, fmt.Errorf("home directory: %w", err)
	}
	preexistingACME := snapshotAcmeDirs(sys, home, opts.PublicIPv4, opts.PublicIPv6)

	issueCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if out, err := runWithContext(issueCtx, sys, acmeSh, issueArgs...); err != nil {
		cleanupAcmeState(sys, acmeSh, opts.PublicIPv4, opts.PublicIPv6, preexistingACME)
		return IssuedCert{}, fmt.Errorf("issue certificate for %s: %w (output: %s)", opts.PublicIPv4, err, string(out))
	}

	installArgs := []string{
		"--installcert",
		"-d", opts.PublicIPv4,
		"--key-file", keyPath,
		"--fullchain-file", certPath,
		"--reloadcmd", renewReloadCmd(certPath, keyPath),
	}
	prevCert, _ := sys.ReadFile(certPath)
	prevKey, _ := sys.ReadFile(keyPath)
	out, installErr := runWithContext(ctx, sys, acmeSh, installArgs...)
	newCert, certErr := sys.ReadFile(certPath)
	newKey, keyErr := sys.ReadFile(keyPath)
	installed := certErr == nil && keyErr == nil && validateIssuedMaterial(newCert, newKey, opts.PublicIPv4, opts.PublicIPv6) == nil &&
		(!bytes.Equal(prevCert, newCert) || !bytes.Equal(prevKey, newKey) || len(prevCert) == 0)
	if !installed {
		if len(prevCert) > 0 {
			_ = sys.WriteFile(certPath, prevCert, 0o644)
		}
		if len(prevKey) > 0 {
			_ = sys.WriteFile(keyPath, prevKey, 0o640)
		}
		if installErr != nil {
			cleanupAcmeState(sys, acmeSh, opts.PublicIPv4, opts.PublicIPv6, preexistingACME)
			return IssuedCert{}, fmt.Errorf("install certificate: %w (output: %s)", installErr, string(out))
		}
		if certErr != nil {
			return IssuedCert{}, fmt.Errorf("install certificate: read %s: %w", certPath, certErr)
		}
		if keyErr != nil {
			return IssuedCert{}, fmt.Errorf("install certificate: read %s: %w", keyPath, keyErr)
		}
		if err := validateIssuedMaterial(newCert, newKey, opts.PublicIPv4, opts.PublicIPv6); err != nil {
			return IssuedCert{}, fmt.Errorf("install certificate: %w", err)
		}
		return IssuedCert{}, fmt.Errorf("install certificate: destination still has previous material")
	}

	if err := fixCertOwnership(sys, certPath, keyPath); err != nil {
		return IssuedCert{}, fmt.Errorf("fix certificate ownership: %w", err)
	}

	return IssuedCert{CertPath: certPath, KeyPath: keyPath}, nil
}

// renewReloadCmd builds the acme.sh --reloadcmd registered at installcert time.
// acme.sh runs it on every renewal and rewrites the key 0600 root:root, so the
// command restores group readability BEFORE restarting the panel — otherwise
// veil.service (User=veil) and the protocol units (User=veil-proxy) can no
// longer read tls.key after renewal (audit #120). chgrp prefers veil-proxy —
// the group both readers share — and falls back to veil when veil-proxy is
// absent; every step fails closed so a renewal can never "succeed" leaving an
// unreadable key or an unrestarted consumer (audit #526).
//
// The hysteria2 loop mirrors postinstall: --plain keeps a status glyph out of
// $1, --state=active skips instances the apply path already stopped/disabled
// (or whose unit file was removed), and try-restart never revives an instance
// that went inactive between listing and restarting — while a genuine restart
// failure on an active unit still aborts the renewal (issue #620).
func renewReloadCmd(certPath, keyPath string) string {
	dir := filepath.Dir(certPath)
	return fmt.Sprintf("chmod 0644 %s && chmod 0640 %s && (chgrp veil-proxy %s %s || chgrp veil %s %s) && (chgrp veil-proxy %s || chgrp veil %s) && chmod 0750 %s && systemctl restart veil.service && for u in $(systemctl list-units --plain --no-legend --state=active 'veil-hysteria2@*.service' | awk '{print $1}'); do systemctl try-restart \"$u\" || exit 1; done",
		shellQuote(certPath), shellQuote(keyPath),
		shellQuote(certPath), shellQuote(keyPath), shellQuote(certPath), shellQuote(keyPath),
		shellQuote(dir), shellQuote(dir), shellQuote(dir))
}

// shellQuote wraps a path in single quotes for embedding in the acme.sh
// reloadcmd string, escaping embedded quotes POSIX-style.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func ensureAcmeSh(ctx context.Context, sys System) (string, error) {
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
	if out, err := runWithContext(ctx, sys, "sh", args...); err != nil {
		return "", fmt.Errorf("install acme.sh: %w (output: %s)", err, string(out))
	}

	if _, err := sys.Stat(acmeSh); err != nil {
		return "", fmt.Errorf("acme.sh did not appear at %s after install", acmeSh)
	}
	return acmeSh, nil
}

// ensureAcmePrereqs verifies the complete toolchain acme.sh needs BEFORE its
// upstream installer runs (audit #298): curl to fetch it, an OpenSSL binary
// for key generation, and a cron scheduler for renewal. Without crontab the
// upstream install refuses to complete, and without OpenSSL key creation
// fails — in both cases the caller silently fell back to self-signed TLS.
func ensureAcmePrereqs(ctx context.Context, sys System) error {
	if _, err := sys.LookPath("curl"); err != nil {
		return fmt.Errorf("curl is required to install acme.sh")
	}
	if err := ensureToolInstalled(ctx, sys, "openssl", func(string) string { return "openssl" }); err != nil {
		return err
	}
	// socat serves the standalone HTTP-01 challenge.
	if err := ensureSocat(ctx, sys); err != nil {
		return fmt.Errorf("socat setup: %w", err)
	}
	if err := ensureToolInstalled(ctx, sys, "crontab", func(manager string) string {
		switch manager {
		case "apt-get":
			return "cron"
		case "apk":
			return "dcron"
		default: // dnf, yum, pacman, zypper
			return "cronie"
		}
	}); err != nil {
		return err
	}
	return nil
}

// distroManagers maps a detected package-manager command to a shell template
// installing the named package (%s is the package list).
var distroManagers = []struct {
	name string
	tmpl string
}{
	{"apt-get", "apt-get update >/dev/null 2>&1 && apt-get install -y %s"},
	{"dnf", "dnf makecache -y >/dev/null 2>&1 && dnf -y install %s"},
	{"yum", "yum makecache -y >/dev/null 2>&1 && yum -y install %s"},
	{"pacman", "pacman -Sy --noconfirm %s"},
	{"zypper", "zypper refresh >/dev/null 2>&1 && zypper -q install -y %s"},
	{"apk", "apk add --no-cache %s"},
}

// ensureToolInstalled guarantees the named executable is in PATH, provisioning
// the distro package that provides it when missing. pkgForManager maps the
// detected manager to the package name; an empty string means that manager
// cannot supply the tool and the next manager is tried.
func ensureToolInstalled(ctx context.Context, sys System, tool string, pkgForManager func(manager string) string) error {
	if _, err := sys.LookPath(tool); err == nil {
		return nil
	}
	for _, m := range distroManagers {
		if _, err := sys.LookPath(m.name); err != nil {
			continue
		}
		pkg := pkgForManager(m.name)
		if pkg == "" {
			continue
		}
		if out, err := runWithContext(ctx, sys, "sh", "-c", fmt.Sprintf(m.tmpl, pkg)); err != nil {
			return fmt.Errorf("install %s via %s: %w (output: %s)", pkg, m.name, err, string(out))
		}
		if _, err := sys.LookPath(tool); err == nil {
			return nil
		}
		return fmt.Errorf("%s installation via %s completed but %s is not in PATH", pkg, m.name, tool)
	}
	return fmt.Errorf("%s is required but not installed, and no supported package manager was found to install it", tool)
}

func ensureSocat(ctx context.Context, sys System) error {
	return ensureToolInstalled(ctx, sys, "socat", func(string) string { return "socat" })
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

// lookupGroupIDFunc allows tests to mock group resolution: running as root in
// an environment without the veil groups must still fail closed.
var lookupGroupIDFunc = lookupGroupID

// chownFunc allows tests to mock file ownership changes.
var chownFunc = os.Chown

func (defaultSystem) Chown(name string, uid, gid int) error {
	return chownFunc(name, uid, gid)
}

// fixCertOwnership makes issued panel cert material readable by the runtime
// group and keeps the cert directory traversable. Protocol units run as
// veil-proxy and share the panel certificate (the panel account is a
// supplementary veil-proxy member), so veil-proxy is preferred and veil is
// the fallback. Every failure propagates: a "successful" install that leaves
// the key unreadable by its consumers is worse than a loud one (audit #528).
func fixCertOwnership(sys System, certPath, keyPath string) error {
	if err := sys.Chmod(certPath, 0o644); err != nil {
		return fmt.Errorf("chmod %s: %w", certPath, err)
	}
	if err := sys.Chmod(keyPath, 0o640); err != nil {
		return fmt.Errorf("chmod %s: %w", keyPath, err)
	}
	if uid := getuidFunc(); uid != 0 {
		// Without root there is no ownership contract to enforce: the files
		// stay owned by the issuing user with the modes set above.
		return nil
	}
	gid := lookupGroupIDFunc("veil-proxy")
	if gid < 0 {
		gid = lookupGroupIDFunc("veil")
	}
	if gid < 0 {
		return fmt.Errorf("resolve veil-proxy or veil group for cert ownership")
	}
	dir := filepath.Dir(certPath)
	if err := sys.Chown(dir, 0, gid); err != nil {
		return fmt.Errorf("chown %s: %w", dir, err)
	}
	if err := sys.Chmod(dir, 0o750); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	if err := sys.Chown(certPath, 0, gid); err != nil {
		return fmt.Errorf("chown %s: %w", certPath, err)
	}
	if err := sys.Chown(keyPath, 0, gid); err != nil {
		return fmt.Errorf("chown %s: %w", keyPath, err)
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

func acmeDomainDirs(home, ip string) []string {
	if ip == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".acme.sh", ip),
		filepath.Join(home, ".acme.sh", ip+"_ecc"),
	}
}

func snapshotAcmeDirs(sys System, home string, ips ...string) map[string]bool {
	existed := map[string]bool{}
	for _, ip := range ips {
		for _, dir := range acmeDomainDirs(home, ip) {
			if _, err := sys.Stat(dir); err == nil {
				existed[dir] = true
			}
		}
	}
	return existed
}

func cleanupAcmeState(sys System, acmeSh, ipv4, ipv6 string, preexisting map[string]bool) {
	home, _ := sys.HomeDir()
	for _, ip := range []string{ipv4, ipv6} {
		if ip == "" {
			continue
		}
		dirs := acmeDomainDirs(home, ip)
		owned := make([]string, 0, len(dirs))
		preserved := false
		for _, dir := range dirs {
			if preexisting[dir] {
				preserved = true
				continue
			}
			owned = append(owned, dir)
		}
		if len(owned) > 0 {
			_ = sys.Run("rm", append([]string{"-rf"}, owned...)...)
		}
		if acmeSh != "" && !preserved {
			_ = sys.Run(acmeSh, "--remove", "-d", ip)
		}
	}
}

func runWithContext(ctx context.Context, sys System, cmd string, args ...string) ([]byte, error) {
	return sys.CombinedOutputContext(ctx, cmd, args...)
}

func validateIssuedMaterial(certPEM, keyPEM []byte, ipv4, ipv6 string) error {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("certificate/key pair: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return fmt.Errorf("certificate/key pair: no certificate")
	}
	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("parse certificate: %w", err)
	}
	now := time.Now()
	if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
		return fmt.Errorf("certificate is not currently valid")
	}
	if err := cert.VerifyHostname(ipv4); err != nil {
		return fmt.Errorf("certificate does not include IP %s: %w", ipv4, err)
	}
	if ipv6 != "" {
		if err := cert.VerifyHostname(ipv6); err != nil {
			return fmt.Errorf("certificate does not include IP %s: %w", ipv6, err)
		}
	}
	return nil
}
