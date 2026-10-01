package acmeip

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

// IssuedCert holds the on-disk paths of a successfully issued certificate.
type IssuedCert struct {
	CertPath string
	KeyPath  string
}

// CaddyFrontedHTTP01Port is the loopback-only port acme.sh's --standalone
// listener parks on when the managed Caddy edge owns the public HTTP-01 port:
// the rendered -acme server (and any other Caddy :80 listener) proxies
// /.well-known/acme-challenge/* to it, so the panel IP certificate renews
// without competing for :80 (#1181). The renderer MUST emit the matching
// reverse-proxy upstream for the same port.
const CaddyFrontedHTTP01Port = 44080

// CaddyFrontedHTTP01Host is the loopback address the parked acme.sh listener
// binds via --local-address (#1207). It lives in the reserved 127.42.0.0/16
// band — deliberately NOT 127.0.0.1 — because no unit-level egress filter
// (veil-warp, veil-hysteria2@, veil-mieru) pierces that band, so a sandboxed
// or broadly-bindable proxy process can neither claim the listener's address
// ahead of acme.sh nor dial it to steal the challenge path. The rendered
// reverse-proxy upstream dials this exact address.
const CaddyFrontedHTTP01Host = "127.42.0.2"

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
	// CARoot is an optional PEM bundle acme.sh verifies the ACME directory
	// endpoint against (--ca-bundle) — the VEIL_ACME_CA_ROOT persisted for
	// controlled-CA installs (#1189).
	CARoot string
	// HTTP01ViaCaddy reports that the managed Caddy edge owns the public
	// HTTP-01 port in the rendered plan (the -acme challenge server or a
	// panel/naive server on :80): when the requested standalone port is busy
	// that is not fatal — acme.sh parks on CaddyFrontedHTTP01Port, which the
	// rendered /.well-known/acme-challenge/ reverse-proxy route forwards to
	// (#1181).
	HTTP01ViaCaddy bool
	// DeferPanelRestart schedules the panel restart the issued certificate's
	// reloadcmd performs instead of running it inline: when the panel itself
	// requested issuance through the privileged helper, a synchronous
	// `systemctl try-restart veil.service` would kill the panel before the
	// helper could answer — or mid-apply (#1170). Callers that are not the
	// panel process (CLI install/repair/renew) leave this false so the
	// restart result stays part of the operation's success contract.
	DeferPanelRestart bool
	// NoCron installs acme.sh with --no-cron: the caller owns the renewal
	// lifecycle (the in-daemon worker, #1170) and the requesting sandbox may
	// not even be able to write a crontab (the helper runs ProtectSystem=
	// strict, so acme.sh's installcronjob would fail the whole --install).
	NoCron bool
	// HomeDir overrides the acme.sh account/install home. The privileged
	// helper runs with ProtectHome=yes, so the default /root/.acme.sh is
	// unreachable; the helper passes a state-rooted home instead.
	HomeDir string
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
	// Lstat must not follow a trailing symlink — the acme.sh execution gate
	// uses it to prove the script path is a real file, not a plant (#1226).
	Lstat(name string) (os.FileInfo, error)
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

func (defaultSystem) Lstat(name string) (os.FileInfo, error) {
	return os.Lstat(name)
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

// homeDirSystem overrides the acme.sh home directory. HomeDir() alone is not
// enough: acme.sh derives its install/account home from the $HOME environment
// variable of the spawned process, so every command the wrapper executes goes
// through `env HOME=<home>` — the privileged helper runs with ProtectHome=yes
// and /root is unreachable, so its acme.sh state must live under the writable
// state root instead (issue #1170). `env` is coreutils and always present on
// hosts that satisfy the issuance prerequisites.
type homeDirSystem struct {
	System
	home string
}

func (s homeDirSystem) HomeDir() (string, error) { return s.home, nil }

func (s homeDirSystem) envArgs(cmd string, args []string) []string {
	return append([]string{"HOME=" + s.home, cmd}, args...)
}

func (s homeDirSystem) Run(cmd string, args ...string) error {
	return s.System.Run("env", s.envArgs(cmd, args)...)
}

func (s homeDirSystem) CombinedOutput(cmd string, args ...string) ([]byte, error) {
	return s.System.CombinedOutput("env", s.envArgs(cmd, args)...)
}

func (s homeDirSystem) CombinedOutputContext(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	return s.System.CombinedOutputContext(ctx, "env", s.envArgs(cmd, args)...)
}

// IssueIPCert obtains a Let's Encrypt shortlived certificate for the given IP
// address(es) using acme.sh standalone mode. It installs acme.sh and socat if
// they are missing, writes the certificate material to CertPath/KeyPath, and
// registers an acme.sh reloadcmd that restarts veil on renewal.
//
// Port 80 (or HTTPPort) must be reachable from the internet — either free for
// the standalone listener or, with HTTP01ViaCaddy, held by the managed Caddy
// edge which proxies the challenge path to the internal port (#1181).
func IssueIPCert(ctx context.Context, opts IssueOptions) (IssuedCert, error) {
	sys := defaultSystemOr(opts.System)
	if home := strings.TrimSpace(opts.HomeDir); home != "" {
		sys = homeDirSystem{System: sys, home: home}
	}

	if opts.PublicIPv4 == "" && opts.PublicIPv6 == "" {
		return IssuedCert{}, fmt.Errorf("a public IPv4 or IPv6 address is required")
	}
	// Family-check each field: an IPv6 literal in PublicIPv4 (or IPv4 in
	// PublicIPv6) silently produced a SAN covering the wrong family while
	// the other stayed uncovered on dual-stack hosts (issue #665).
	if opts.PublicIPv4 != "" {
		if ip := net.ParseIP(opts.PublicIPv4); ip == nil || ip.To4() == nil {
			return IssuedCert{}, fmt.Errorf("public IPv4 %q is not a valid IPv4 address", opts.PublicIPv4)
		}
	}
	if opts.PublicIPv6 != "" {
		if ip := net.ParseIP(opts.PublicIPv6); ip == nil || ip.To4() != nil {
			return IssuedCert{}, fmt.Errorf("public IPv6 %q is not a valid IPv6 address", opts.PublicIPv6)
		}
	}
	// acme.sh addresses the issued certificate by its first -d identity;
	// prefer IPv4 when both are present, fall back to IPv6-only issuance.
	primaryName := opts.PublicIPv4
	if primaryName == "" {
		primaryName = opts.PublicIPv6
	}

	certPath := opts.CertPath
	if certPath == "" {
		certPath = filepath.Join(hostenv.EtcDir(), "panel", "tls.crt")
	}
	keyPath := opts.KeyPath
	if keyPath == "" {
		keyPath = filepath.Join(hostenv.EtcDir(), "panel", "tls.key")
	}
	httpPort := opts.HTTPPort
	if httpPort <= 0 {
		httpPort = 80
	}

	if err := ensureAcmePrereqsMode(ctx, sys, opts.NoCron); err != nil {
		return IssuedCert{}, fmt.Errorf("acme prerequisites: %w", err)
	}
	acmeSh, err := ensureAcmeShMode(ctx, sys, opts.NoCron)
	if err != nil {
		return IssuedCert{}, fmt.Errorf("acme.sh setup: %w", err)
	}
	caddyParked := false
	if opts.HTTP01ViaCaddy && !sys.IsPortFree(80) {
		// The managed Caddy edge owns the public HTTP-01 port — typically the
		// permanent -acme challenge server a hysteria2-domain plan renders on
		// :80 — so acme.sh --standalone can never bind it (#1181). The
		// rendered /.well-known/acme-challenge/ reverse-proxy route forwards
		// every non-certmagic token to the parked listener, so standalone
		// parks on the dedicated loopback address and the public :80 path
		// keeps working end to end. The probe targets the PUBLIC edge port —
		// not the operator's --le-ip-cert-port override — because the Caddy
		// route unconditionally forwards to the internal port (#1208). When
		// the public port is still free (Caddy stopped mid-restart)
		// standalone simply binds the requested port directly.
		httpPort = CaddyFrontedHTTP01Port
		caddyParked = true
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
		"-d", primaryName,
		"--standalone",
		"--server", caServer,
	}
	if caServer == "letsencrypt" {
		// Let's Encrypt's shortlived profile is required for IP certificates;
		// other CAs (e.g. a controlled test CA) reject profile/days overrides.
		issueArgs = append(issueArgs, "--certificate-profile", "shortlived", "--days", "3")
	}
	issueArgs = append(issueArgs, "--httpport", strconv.Itoa(httpPort))
	if caddyParked {
		// Bind only the reserved-band loopback address the rendered route
		// dials — a wildcard listener on the internal port would be
		// claimable/servable by any local process between issuance runs
		// (#1207).
		issueArgs = append(issueArgs, "--local-address", CaddyFrontedHTTP01Host)
	}
	issueArgs = append(issueArgs, "--force")
	if opts.Insecure {
		issueArgs = append(issueArgs, "--insecure")
	}
	if caRoot := strings.TrimSpace(opts.CARoot); caRoot != "" {
		// Controlled CA whose directory endpoint is not publicly trusted:
		// the install persisted VEIL_ACME_CA_ROOT so acme.sh can verify it
		// (#1189).
		issueArgs = append(issueArgs, "--ca-bundle", caRoot)
	}
	if opts.PublicIPv6 != "" && opts.PublicIPv6 != primaryName {
		issueArgs = append(issueArgs, "-d", opts.PublicIPv6)
	}
	if opts.PublicIPv4 != "" && opts.PublicIPv4 != primaryName {
		issueArgs = append(issueArgs, "-d", opts.PublicIPv4)
	}
	if email := strings.TrimSpace(opts.Email); email != "" {
		// IssueOptions.Email was silently dropped: -m exports ACCOUNT_EMAIL,
		// which acme.sh embeds as the ACME account contact during the
		// implicit registration inside --issue (issue #1114).
		issueArgs = append(issueArgs, "-m", email)
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
		return IssuedCert{}, fmt.Errorf("issue certificate for %s: %w (output: %s)", primaryName, err, string(out))
	}

	reloadCmd := renewReloadCmd(certPath, keyPath)
	if opts.DeferPanelRestart {
		reloadCmd = renewReloadCmdDeferredPanelRestart(certPath, keyPath)
	}
	installArgs := []string{
		"--installcert",
		"-d", primaryName,
		"--key-file", keyPath,
		"--fullchain-file", certPath,
		"--reloadcmd", reloadCmd,
	}
	prevCert := snapshotFileState(sys, certPath)
	prevKey := snapshotFileState(sys, keyPath)
	out, installErr := runWithContext(ctx, sys, acmeSh, installArgs...)
	newCert, certErr := sys.ReadFile(certPath)
	newKey, keyErr := sys.ReadFile(keyPath)
	installed := certErr == nil && keyErr == nil && validateIssuedMaterial(newCert, newKey, opts.PublicIPv4, opts.PublicIPv6) == nil &&
		(!bytes.Equal(prevCert.data, newCert) || !bytes.Equal(prevKey.data, newKey) || len(prevCert.data) == 0)
	if !installed {
		// Rollback restores bytes AND the original ownership/mode: WriteFile
		// alone would leave whatever metadata acme.sh's installcert left
		// behind, which can differ from the operator-managed predecessor
		// (#1208). A failed restore surfaces — a panel left running on
		// mis-owned material must not look like a clean failure.
		var primary error
		switch {
		case installErr != nil:
			cleanupAcmeState(sys, acmeSh, opts.PublicIPv4, opts.PublicIPv6, preexistingACME)
			primary = fmt.Errorf("install certificate: %w (output: %s)", installErr, string(out))
		case certErr != nil:
			primary = fmt.Errorf("install certificate: read %s: %w", certPath, certErr)
		case keyErr != nil:
			primary = fmt.Errorf("install certificate: read %s: %w", keyPath, keyErr)
		default:
			if err := validateIssuedMaterial(newCert, newKey, opts.PublicIPv4, opts.PublicIPv6); err != nil {
				primary = fmt.Errorf("install certificate: %w", err)
			} else {
				primary = fmt.Errorf("install certificate: destination still has previous material")
			}
		}
		if err := errors.Join(prevCert.restore(sys, certPath), prevKey.restore(sys, keyPath)); err != nil {
			primary = fmt.Errorf("%w (rollback restore: %v)", primary, err)
		}
		return IssuedCert{}, primary
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
// longer read tls.key after renewal (audit #120). The chgrp targets veil-proxy
// unconditionally: EnsureAccount provisions that group during install and the
// protocol units need it specifically — a `chgrp veil` fallback would let a
// broken provisioning state report a successful renewal while veil-proxy units
// lose key readability (issue #760). Every step fails closed so a renewal can
// never "succeed" leaving an unreadable key or an unrestarted consumer
// (audit #526).
//
// The hysteria2 loop mirrors postinstall: --plain keeps a status glyph out of
// $1, --state=active skips instances the apply path already stopped/disabled
// (or whose unit file was removed), and try-restart never revives an instance
// that went inactive between listing and restarting — while a genuine restart
// failure on an active unit still aborts the renewal (issue #620).
//
// veil.service itself gets the same treatment: a cert renewal must never
// revive a panel the operator deliberately stopped, so `try-restart` restarts
// it only when it is already active — while still failing closed when the
// restart of an active panel genuinely fails (issue #999).
func renewReloadCmd(certPath, keyPath string) string {
	return buildRenewReloadCmd(certPath, keyPath, "systemctl try-restart veil.service")
}

// renewReloadCmdDeferredPanelRestart is the variant used when the panel
// itself requested issuance through the privileged helper (#1170). The panel
// restart rides a transient systemd timer so the helper can finish answering
// the in-flight request first; a synchronous try-restart would kill the
// caller mid-response — or mid-apply. Scheduling failure still fails the
// reload command closed, so a renewed cert can never sit unloaded without
// the caller hearing about it.
func renewReloadCmdDeferredPanelRestart(certPath, keyPath string) string {
	return buildRenewReloadCmd(certPath, keyPath,
		"systemd-run --quiet --collect --on-active=2 --unit veil-ip-cert-panel-restart -- systemctl try-restart veil.service")
}

func buildRenewReloadCmd(certPath, keyPath, panelRestart string) string {
	dir := filepath.Dir(certPath)
	return fmt.Sprintf("chmod 0644 %s && chmod 0640 %s && chgrp veil-proxy %s %s && chgrp veil-proxy %s && chmod 0750 %s && %s && for u in $(systemctl list-units --plain --no-legend --state=active 'veil-hysteria2@*.service' | awk '{print $1}'); do systemctl try-restart \"$u\" || exit 1; done",
		shellQuote(certPath), shellQuote(keyPath),
		shellQuote(certPath), shellQuote(keyPath),
		shellQuote(dir), shellQuote(dir),
		panelRestart)
}

// shellQuote wraps a path in single quotes for embedding in the acme.sh
// reloadcmd string, escaping embedded quotes POSIX-style.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// acme.sh is pinned to a fixed release instead of `curl get.acme.sh | sh`:
// the pipe executed whatever GitHub's redirect endpoint returned as root.
// Now a fixed tag tarball is downloaded, its SHA-256 is verified against the
// pinned digest, and only the verified tree is installed (issue #1115).
const acmeShVersion = "3.1.6"

// SHA-256 of https://github.com/acmesh-official/acme.sh/archive/refs/tags/3.1.6.tar.gz
const acmeShTarballSHA256 = "0d3f9000ac44a6331314742a88c475f79134e24fc991997883652adc59efc486"

const acmeShTarballURL = "https://github.com/acmesh-official/acme.sh/archive/refs/tags/" + acmeShVersion + ".tar.gz"

// acmeShInstallScript downloads the pinned release archive into a private
// tempdir, verifies its digest, and runs the upstream installer from the
// extracted tree — acme.sh's --install resolves sibling files relative to
// its own path, so it must run from inside the release directory, not via a
// pipe. The tmpdir is removed on exit.
func acmeShInstallScript() string {
	return acmeShInstallScriptMode(false)
}

// acmeShInstallScriptMode renders the pinned-release install script. noCron
// maps to acme.sh --no-cron (plus --no-profile so no shell rcfiles are
// touched): renewal is driven by Veil's own scheduler in that mode, and the
// helper sandbox cannot write a crontab anyway (ProtectSystem=strict).
func acmeShInstallScriptMode(noCron bool) string {
	installArgs := "--install"
	if noCron {
		installArgs += " --no-cron --no-profile"
	}
	return `set -e
mkdir -p "$HOME" && chmod 0700 "$HOME"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
curl -fsSL "` + acmeShTarballURL + `" -o "$tmpdir/acme.sh.tar.gz"
echo "` + acmeShTarballSHA256 + `  $tmpdir/acme.sh.tar.gz" | sha256sum -c -
tar -xzf "$tmpdir/acme.sh.tar.gz" -C "$tmpdir"
cd "$tmpdir/acme.sh-` + acmeShVersion + `"
NO_DETECT_SH=1 sh ./acme.sh ` + installArgs
}

// acmeShScriptSHA256 is the SHA-256 of the acme.sh payload inside the pinned
// tarball. The installer runs with NO_DETECT_SH=1 so upstream --install never
// rewrites the shebang — the file it places at <home>/.acme.sh/acme.sh keeps
// exactly these bytes, which lets the trust gate below prove provenance by
// digest instead of trusting mere presence. It is a var so tests can point
// the gate at their fake payload.
var acmeShScriptSHA256 = "c7d68b021cfd6380ea83a82962abde5b484779fee0b97d38681dfa1396bbc8d7"

// trustedAcmeSh reports whether the existing acme.sh at acmeSh (under home)
// is safe for root to exec: a non-symlink regular file, not group/other
// writable, root-owned when the caller can check ownership, inside dirs that
// are themselves root-owned and not group/other-writable, AND byte-identical
// to the pinned release payload. The legacy /var/lib/veil/acme home was
// service-writable, so "the file exists" alone let a compromised veil-uid
// process hand root an arbitrary script to run (issue #1226).
func trustedAcmeSh(sys System, home, acmeSh string) bool {
	if !rootOwnedDir(sys, home) || !rootOwnedDir(sys, filepath.Dir(acmeSh)) {
		return false
	}
	fi, err := sys.Lstat(acmeSh)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o022 != 0 || fi.Mode().Perm()&0o100 == 0 {
		return false
	}
	if getuidFunc() == 0 && !fileOwnedByUID(fi, 0) {
		return false
	}
	payload, err := sys.ReadFile(acmeSh)
	if err != nil {
		return false
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]) == acmeShScriptSHA256
}

// rootOwnedDir reports whether path is a directory that group/other users
// cannot write and — when running as root — that is owned by uid 0. It is
// the structural half of the acme.sh trust gate: a writable home means every
// file inside it is forgeable by the directory's owner (#1226).
func rootOwnedDir(sys System, path string) bool {
	fi, err := sys.Lstat(path)
	if err != nil || !fi.IsDir() || fi.Mode().Perm()&0o022 != 0 {
		return false
	}
	return getuidFunc() != 0 || fileOwnedByUID(fi, 0)
}

func ensureAcmeSh(ctx context.Context, sys System) (string, error) {
	return ensureAcmeShMode(ctx, sys, false)
}

func ensureAcmeShMode(ctx context.Context, sys System, noCron bool) (string, error) {
	home, err := sys.HomeDir()
	if err != nil {
		return "", fmt.Errorf("home directory: %w", err)
	}
	acmeDir := filepath.Join(home, ".acme.sh")
	acmeSh := filepath.Join(acmeDir, "acme.sh")
	if trustedAcmeSh(sys, home, acmeSh) {
		return acmeSh, nil
	}

	// Anything already sitting on those paths failed the trust gate, so it
	// is untrusted input rather than a partial install to skip: a symlinked
	// or foreign-owned directory is removed outright (healing in place would
	// still leave the reinstall writing into attacker-chosen space), while a
	// real directory merely loses its group/other write and gains root
	// ownership before the reinstall overwrites the payload (#1226).
	for _, dir := range []string{home, acmeDir} {
		fi, err := sys.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("stat acme home %s: %w", dir, err)
		}
		if !fi.IsDir() {
			_ = sys.Run("rm", "-rf", dir)
			continue
		}
		if fi.Mode().Perm()&0o022 != 0 {
			_ = sys.Chmod(dir, 0o700)
		}
		if getuidFunc() == 0 && !fileOwnedByUID(fi, 0) {
			_ = sys.Chown(dir, 0, 0)
		}
	}
	if _, err := sys.Lstat(acmeSh); err == nil {
		// A file, directory, or symlink with the wrong digest/mode/owner
		// never reaches exec; rm first so the installer's cp cannot be
		// steered through a surviving link.
		_ = sys.Run("rm", "-rf", acmeSh)
	}

	for _, tool := range []string{"curl", "sha256sum", "tar", "mktemp"} {
		if _, err := sys.LookPath(tool); err != nil {
			return "", fmt.Errorf("%s is required to install acme.sh", tool)
		}
	}

	if out, err := runWithContext(ctx, sys, "sh", "-c", acmeShInstallScriptMode(noCron)); err != nil {
		return "", fmt.Errorf("install acme.sh %s: %w (output: %s)", acmeShVersion, err, string(out))
	}

	// The reinstall must produce a fully trusted file — a directory that
	// could not be healed, a surviving symlink, or a payload that is not the
	// pinned release all fail closed here instead of exec'ing.
	if !trustedAcmeSh(sys, home, acmeSh) {
		return "", fmt.Errorf("installed %s fails provenance or ownership validation", acmeSh)
	}
	return acmeSh, nil
}

// ensureAcmePrereqs verifies the complete toolchain acme.sh needs BEFORE its
// upstream installer runs (audit #298): curl to fetch it, an OpenSSL binary
// for key generation, and a cron scheduler for renewal. Without crontab the
// upstream install refuses to complete, and without OpenSSL key creation
// fails — in both cases the caller silently fell back to self-signed TLS.
func ensureAcmePrereqs(ctx context.Context, sys System) error {
	return ensureAcmePrereqsMode(ctx, sys, false)
}

func ensureAcmePrereqsMode(ctx context.Context, sys System, noCron bool) error {
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
	if noCron {
		// Renewal is owned by the caller's own scheduler (the in-daemon
		// worker, #1170), so no crontab is needed — and the helper's
		// ProtectSystem=strict sandbox could not install one anyway.
		return nil
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

// fileStateSnapshot captures a file's bytes plus its ownership/mode metadata
// so the installcert rollback restores the predecessor completely — bytes
// alone would silently keep whatever mode/owner acme.sh left (#1208).
type fileStateSnapshot struct {
	data   []byte
	mode   os.FileMode
	uid    int
	gid    int
	haveID bool
	ok     bool
}

func snapshotFileState(sys System, path string) fileStateSnapshot {
	var snap fileStateSnapshot
	data, err := sys.ReadFile(path)
	if err != nil || len(data) == 0 {
		return snap
	}
	snap.data = data
	snap.ok = true
	if fi, err := sys.Stat(path); err == nil {
		snap.mode = fi.Mode().Perm()
		snap.uid, snap.gid, snap.haveID = fileOwnership(fi)
	}
	return snap
}

func (snap fileStateSnapshot) restore(sys System, path string) error {
	if !snap.ok {
		return nil
	}
	mode := snap.mode
	if mode == 0 {
		mode = 0o644
	}
	if err := sys.WriteFile(path, snap.data, mode); err != nil {
		return fmt.Errorf("rollback %s: %w", path, err)
	}
	// WriteFile leaves a pre-existing file's mode untouched and never sets
	// ownership — restore both explicitly.
	if err := sys.Chmod(path, mode); err != nil {
		return fmt.Errorf("rollback %s mode: %w", path, err)
	}
	if snap.haveID {
		if err := sys.Chown(path, snap.uid, snap.gid); err != nil {
			return fmt.Errorf("rollback %s ownership: %w", path, err)
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
	if ipv4 != "" {
		if err := cert.VerifyHostname(ipv4); err != nil {
			return fmt.Errorf("certificate does not include IP %s: %w", ipv4, err)
		}
	}
	if ipv6 != "" {
		if err := cert.VerifyHostname(ipv6); err != nil {
			return fmt.Errorf("certificate does not include IP %s: %w", ipv6, err)
		}
	}
	return nil
}
