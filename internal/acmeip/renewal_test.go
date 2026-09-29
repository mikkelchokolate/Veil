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
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTestCertPEM writes a self-signed certificate whose issuer carries the
// supplied organization — the installer fallback shape (#1169/#1170).
func writeTestCertPEM(t *testing.T, path, issuerOrg string, notAfter time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "veil-test", Organization: []string{issuerOrg}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeTestCACertPEM writes a leaf signed by a freshly generated CA whose
// subject carries issuerOrg — the shape NeedsRenewal accepts as ACME-managed
// (#1185). IP SANs are attached so ManagedCertIPIdentities can be exercised.
func writeTestCACertPEM(t *testing.T, path, issuerOrg string, notAfter time.Time, ips ...net.IP) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial := func() *big.Int {
		s, err := rand.Int(rand.Reader, big.NewInt(1<<62))
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	caTemplate := x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: issuerOrg + " Test Root", Organization: []string{issuerOrg}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: "veil-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, &leafTemplate, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNeedsRenewalMissingOrUnparsableCert(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if !NeedsRenewal(filepath.Join(dir, "missing.crt"), now) {
		t.Fatal("missing certificate must need renewal")
	}
	garbage := filepath.Join(dir, "garbage.crt")
	if err := os.WriteFile(garbage, []byte("not a pem"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !NeedsRenewal(garbage, now) {
		t.Fatal("unparsable certificate must need renewal")
	}
}

func TestNeedsRenewalNonLetsEncryptIssuer(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	// The installer's self-signed fallback: a certificate that is technically
	// valid but not issued by Let's Encrypt must still be replaced (#1169).
	writeTestCertPEM(t, certPath, "Veil Self-Signed", time.Now().Add(90*24*time.Hour))
	if !NeedsRenewal(certPath, time.Now()) {
		t.Fatal("self-signed certificate must need renewal")
	}
}

func TestNeedsRenewalHealthyCertOutsideWindow(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	writeTestCACertPEM(t, certPath, "Let's Encrypt", time.Now().Add(6*24*time.Hour))
	if NeedsRenewal(certPath, time.Now()) {
		t.Fatal("fresh Let's Encrypt certificate must not need renewal")
	}
}

// Issue #1185: a valid leaf issued by a controlled/private ACME CA (Pebble,
// Smallstep, ZeroSSL — any issuer that is not Let's Encrypt) is just as
// managed as a Let's Encrypt certificate. Gating on the issuer brand made a
// healthy private-CA certificate renew in a loop.
func TestNeedsRenewalPrivateCAOutsideWindow(t *testing.T) {
	dir := t.TempDir()
	for _, issuer := range []string{"Pebble", "Smallstep", "ZeroSSL", "Example Internal CA"} {
		certPath := filepath.Join(dir, issuer+".crt")
		writeTestCACertPEM(t, certPath, issuer, time.Now().Add(6*24*time.Hour))
		if NeedsRenewal(certPath, time.Now()) {
			t.Fatalf("fresh %q-issued certificate must not need renewal", issuer)
		}
	}
	// ... but a private-CA leaf inside the renewal window still renews.
	certPath := filepath.Join(dir, "expiring.crt")
	writeTestCACertPEM(t, certPath, "Pebble", time.Now().Add(24*time.Hour))
	if !NeedsRenewal(certPath, time.Now()) {
		t.Fatal("private-CA certificate inside the window must still renew")
	}
}

// Caddy's internal local CA classifies as "internal", not ACME-managed:
// a local-CA leaf must still be replaced by real issuance.
func TestNeedsRenewalCaddyInternalCA(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	writeTestCACertPEM(t, certPath, "Caddy Local Authority", time.Now().Add(90*24*time.Hour))
	if !NeedsRenewal(certPath, time.Now()) {
		t.Fatal("Caddy-local-CA certificate must need renewal")
	}
}

func TestNeedsRenewalInsideWindowAndExpired(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	inside := filepath.Join(dir, "inside.crt")
	writeTestCACertPEM(t, inside, "Let's Encrypt", now.Add(48*time.Hour))
	if !NeedsRenewal(inside, now) {
		t.Fatal("certificate expiring within the renewal window must need renewal")
	}

	expired := filepath.Join(dir, "expired.crt")
	writeTestCACertPEM(t, expired, "Let's Encrypt", now.Add(-time.Hour))
	if !NeedsRenewal(expired, now) {
		t.Fatal("expired certificate must need renewal")
	}
}

func TestNeedsRenewalHonorsInjectedClock(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	// 5 days out: healthy now, inside the window once the injected clock
	// advances past the 72h boundary.
	writeTestCACertPEM(t, certPath, "Let's Encrypt", time.Now().Add(5*24*time.Hour))
	if NeedsRenewal(certPath, time.Now()) {
		t.Fatal("certificate 5 days out must not need renewal")
	}
	if !NeedsRenewal(certPath, time.Now().Add(49*time.Hour)) {
		t.Fatal("injected clock inside the renewal window must trigger renewal")
	}
}

func TestParsePublicIPSpec(t *testing.T) {
	v4, v6, err := ParsePublicIPSpec("203.0.113.9,2001:db8::7")
	if err != nil || v4 != "203.0.113.9" || v6 != "2001:db8::7" {
		t.Fatalf("dual spec = %q/%q err=%v", v4, v6, err)
	}
	v4, v6, err = ParsePublicIPSpec("2001:db8::7")
	if err != nil || v4 != "" || v6 != "2001:db8::7" {
		t.Fatalf("v6-only spec = %q/%q err=%v", v4, v6, err)
	}
	v4, v6, err = ParsePublicIPSpec(" 203.0.113.9 ")
	if err != nil || v4 != "203.0.113.9" || v6 != "" {
		t.Fatalf("v4-only spec = %q/%q err=%v", v4, v6, err)
	}
	for _, bad := range []string{"not-an-ip", "auto", "1.2.3.4,5.6.7.8", "2001:db8::1,2001:db8::2"} {
		if _, _, err := ParsePublicIPSpec(bad); err == nil {
			t.Fatalf("spec %q must be rejected", bad)
		}
	}
}

func TestManagedCertIPIdentities(t *testing.T) {
	dir := t.TempDir()
	managed := filepath.Join(dir, "managed.crt")
	writeTestCACertPEM(t, managed, "Example Internal CA", time.Now().Add(24*time.Hour),
		net.ParseIP("203.0.113.9"), net.ParseIP("2001:db8::7"))
	sans := ManagedCertIPIdentities(managed)
	if len(sans) != 2 || sans[0] != "203.0.113.9" || sans[1] != "2001:db8::7" {
		t.Fatalf("managed cert SANs = %v", sans)
	}

	// Self-signed fallback material carries interface/loopback IPs that must
	// never steer issuance (#1186).
	selfSigned := filepath.Join(dir, "self.crt")
	writeTestCertPEM(t, selfSigned, "Veil Self-Signed", time.Now().Add(24*time.Hour))
	if ips := ManagedCertIPIdentities(selfSigned); len(ips) != 0 {
		t.Fatalf("self-signed cert must not yield renewal identities: %v", ips)
	}
	if ips := ManagedCertIPIdentities(filepath.Join(dir, "missing.crt")); ips != nil {
		t.Fatalf("missing cert must yield no identities: %v", ips)
	}
}

func TestResolvePublicIPsExplicitFamilies(t *testing.T) {
	oldResolver, oldProber := publicIPResolver, publicIPFamilyProber
	defer func() { publicIPResolver, publicIPFamilyProber = oldResolver, oldProber }()
	probed := false
	publicIPResolver = func(_ context.Context, value string, _ *http.Client, _ []string) (net.IP, error) {
		return net.ParseIP(value), nil
	}
	publicIPFamilyProber = func(context.Context, []string, string) (net.IP, error) {
		probed = true
		return nil, errors.New("must not be called for explicit addresses")
	}

	v4, v6, err := ResolvePublicIPs(context.Background(), "1.2.3.4")
	if err != nil || v4 != "1.2.3.4" || v6 != "" {
		t.Fatalf("explicit IPv4 resolved to v4=%q v6=%q err=%v", v4, v6, err)
	}
	v4, v6, err = ResolvePublicIPs(context.Background(), "2001:db8::5")
	if err != nil || v4 != "" || v6 != "2001:db8::5" {
		t.Fatalf("explicit IPv6 resolved to v4=%q v6=%q err=%v", v4, v6, err)
	}
	if probed {
		t.Fatal("explicit literal must never trigger the other-family probe")
	}
}

func TestResolvePublicIPsAutoProbesOtherFamily(t *testing.T) {
	oldResolver, oldProber := publicIPResolver, publicIPFamilyProber
	defer func() { publicIPResolver, publicIPFamilyProber = oldResolver, oldProber }()
	publicIPResolver = func(_ context.Context, _ string, _ *http.Client, _ []string) (net.IP, error) {
		return net.ParseIP("203.0.113.9"), nil
	}
	var wantNetwork string
	publicIPFamilyProber = func(_ context.Context, _ []string, network string) (net.IP, error) {
		wantNetwork = network
		return net.ParseIP("2001:db8::7"), nil
	}
	v4, v6, err := ResolvePublicIPs(context.Background(), "auto")
	if err != nil {
		t.Fatalf("auto detection failed: %v", err)
	}
	if v4 != "203.0.113.9" || v6 != "2001:db8::7" {
		t.Fatalf("dual-stack resolution = %q/%q", v4, v6)
	}
	if wantNetwork != "tcp6" {
		t.Fatalf("IPv4-primary detection must probe tcp6, probed %q", wantNetwork)
	}
}

func TestResolvePublicIPsAutoIPv6PrimaryProbesIPv4(t *testing.T) {
	oldResolver, oldProber := publicIPResolver, publicIPFamilyProber
	defer func() { publicIPResolver, publicIPFamilyProber = oldResolver, oldProber }()
	publicIPResolver = func(_ context.Context, _ string, _ *http.Client, _ []string) (net.IP, error) {
		return net.ParseIP("2001:db8::7"), nil
	}
	var wantNetwork string
	publicIPFamilyProber = func(_ context.Context, _ []string, network string) (net.IP, error) {
		wantNetwork = network
		return net.ParseIP("198.51.100.3"), nil
	}
	v4, v6, err := ResolvePublicIPs(context.Background(), "")
	if err != nil {
		t.Fatalf("auto detection failed: %v", err)
	}
	if v4 != "198.51.100.3" || v6 != "2001:db8::7" {
		t.Fatalf("dual-stack resolution = %q/%q", v4, v6)
	}
	if wantNetwork != "tcp4" {
		t.Fatalf("IPv6-primary detection must probe tcp4, probed %q", wantNetwork)
	}
}

func TestResolvePublicIPsProbeFailureKeepsSingleFamily(t *testing.T) {
	oldResolver, oldProber := publicIPResolver, publicIPFamilyProber
	defer func() { publicIPResolver, publicIPFamilyProber = oldResolver, oldProber }()
	publicIPResolver = func(_ context.Context, _ string, _ *http.Client, _ []string) (net.IP, error) {
		return net.ParseIP("203.0.113.9"), nil
	}
	publicIPFamilyProber = func(context.Context, []string, string) (net.IP, error) {
		return nil, errors.New("no IPv6 connectivity")
	}
	v4, v6, err := ResolvePublicIPs(context.Background(), "auto")
	if err != nil || v4 != "203.0.113.9" || v6 != "" {
		t.Fatalf("single-stack resolution = %q/%q err=%v", v4, v6, err)
	}
}

func TestResolvePublicIPsResolverErrorPropagates(t *testing.T) {
	oldResolver := publicIPResolver
	defer func() { publicIPResolver = oldResolver }()
	publicIPResolver = func(context.Context, string, *http.Client, []string) (net.IP, error) {
		return nil, errors.New("all endpoints unreachable")
	}
	if _, _, err := ResolvePublicIPs(context.Background(), "auto"); err == nil {
		t.Fatal("resolver failure must propagate")
	}
}

func TestIssueIPCertNoCronSkipsCronProvisioning(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"
	// No crontab in lookPaths and cron must never be installed in NoCron mode.
	delete(sys.lookPaths, "crontab")
	sys.installCron = false

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{out: "Installed"}

	if _, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys, NoCron: true}); err != nil {
		t.Fatalf("NoCron issuance must not require crontab: %v", err)
	}
}

func TestIssueIPCertNoCronInstallsAcmeShWithoutCron(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key("sh", "-c", acmeShInstallScriptMode(true))] = commandResult{out: "installed"}
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{out: "Installed"}

	if _, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys, NoCron: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIssueIPCertDeferPanelRestartUsesTransientTimer(t *testing.T) {
	sys := newFakeSystem()
	sys.setAcmeInstalled()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	acmeSh := filepath.Join(sys.home, ".acme.sh", "acme.sh")
	sys.commands[sys.key(acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key(acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	deferred := renewReloadCmdDeferredPanelRestart("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key")
	sys.commands[sys.key(acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", deferred)] = commandResult{out: "Installed"}

	if _, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys, DeferPanelRestart: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The deferred reloadcmd must schedule try-restart through systemd-run —
	// a synchronous try-restart would kill the requesting panel before the
	// helper could answer (#1170).
	if !containsAll(deferred, "systemd-run", "--on-active", "try-restart", "veil.service") {
		t.Fatalf("deferred reloadcmd lost the transient timer: %s", deferred)
	}
	if !containsAll(renewReloadCmd("/a", "/b"), "systemctl try-restart veil.service") {
		t.Fatal("default reloadcmd lost the synchronous restart")
	}
}

func TestIssueIPCertHomeDirWrapsExecWithEnvHome(t *testing.T) {
	sys := newFakeSystem()
	sys.lookPaths["socat"] = "/usr/bin/socat"

	// With HomeDir the acme.sh path lives under the override and every
	// spawned command runs via `env HOME=<home>` so acme.sh's own $HOME
	// resolution cannot escape to /root (ProtectHome=yes in the helper,
	// #1170).
	home := "/var/lib/veil/acme"
	acmeSh := filepath.Join(home, ".acme.sh", "acme.sh")
	sys.commands[sys.key("env", "HOME="+home, "sh", "-c", acmeShInstallScriptMode(true))] = commandResult{out: "installed"}
	sys.commands[sys.key("env", "HOME="+home, acmeSh, "--set-default-ca", "--server", "letsencrypt")] = commandResult{out: "OK"}
	sys.commands[sys.key("env", "HOME="+home, acmeSh, "--issue", "-d", "1.2.3.4", "--standalone", "--server", "letsencrypt", "--certificate-profile", "shortlived", "--days", "3", "--httpport", "80", "--force")] = commandResult{out: "Cert issued"}
	sys.commands[sys.key("env", "HOME="+home, acmeSh, "--installcert", "-d", "1.2.3.4", "--key-file", "/etc/veil/panel/tls.key", "--fullchain-file", "/etc/veil/panel/tls.crt", "--reloadcmd", renewReloadCmd("/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key"))] = commandResult{out: "Installed"}

	if _, err := IssueIPCert(context.Background(), IssueOptions{PublicIPv4: "1.2.3.4", System: sys, HomeDir: home, NoCron: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No exec call may have run without the env HOME= prefix.
	for _, call := range sys.execCalls {
		if !strings.HasPrefix(call, "env ") {
			t.Fatalf("command ran without HOME override: %q", call)
		}
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			return false
		}
	}
	return true
}
