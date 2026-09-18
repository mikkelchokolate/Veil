package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/caddycapabilities"
	installflow "github.com/mikkelchokolate/Veil/internal/cliflow/install"
	statusflow "github.com/mikkelchokolate/Veil/internal/cliflow/status"
	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/hostaccess"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/service"
	"github.com/mikkelchokolate/Veil/internal/statecommit"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
)

var installSystemdRunFunc = func(actions []service.SystemdAction) error {
	return service.RunSystemdActions(service.ExecRunner{}, actions)
}

var leIPCertIssueFunc = func(ctx context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
	return acmeip.IssueIPCert(ctx, opts)
}

var installExecutableFunc = os.Executable
var installPrepareHostFunc = hostaccess.Prepare
var installWaitPanelReadyFunc = waitForInstalledPanelReady
var installHelperSocketPath = "/run/veil/helper.sock"
var installPanelReadyTimeout = 15 * time.Second
var installFirewallApplyFunc = func(rules []firewall.Rule) error {
	// Firewall management requires root. In tests and staging installs that run
	// as an unprivileged user we silently skip applying rules rather than fail.
	if os.Geteuid() != 0 {
		return nil
	}
	return firewall.NewUFWApplier().ApplySafely(rules)
}

// installEnsureFirewallBackendFunc runs the firewall dependency phase before
// the install mutates host state or downloads runtimes (audit #295).
var installEnsureFirewallBackendFunc = ensureInstallFirewallBackend

// installGeteuidFunc reports the effective uid; tests stub it to exercise the
// root-only firewall dependency phase.
var installGeteuidFunc = os.Geteuid

// activeForeignFirewallFunc reports the name of a different firewall service
// already managing the host ("" when none is active).
var activeForeignFirewallFunc = func(ctx context.Context) string {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, unit := range []string{"firewalld.service", "nftables.service", "iptables.service"} {
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := exec.CommandContext(probeCtx, "systemctl", "is-active", "--quiet", unit).Run()
		cancel()
		if err == nil {
			return strings.TrimSuffix(unit, ".service")
		}
	}
	return ""
}

// provisionUFWCommandFunc executes one package-manager command for ufw
// provisioning; tests stub it to record calls.
var provisionUFWCommandFunc = func(ctx context.Context, name string, args ...string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w (output: %s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

var provisionUFWFunc = provisionUFW

// ensureInstallFirewallBackend chooses and validates the firewall strategy
// before state, runtimes, or downloads touch the host. Direct/caddy installs
// produce a ufw plan; when ufw is absent it is provisioned through the distro
// package manager — unless a different firewall is already active, in which
// case silently installing a competing backend would not open the ports.
func ensureInstallFirewallBackend(ctx context.Context, profile installer.RURecommendedProfile, opts ruRecommendedInstallOptions) error {
	if installGeteuidFunc() != 0 {
		// Non-root installs cannot mutate the firewall; the apply step already
		// skips rule application for them.
		return nil
	}
	plan, err := buildInstallPlan(profile, opts)
	if err != nil {
		return err
	}
	if len(plan.FirewallActions) == 0 {
		return nil
	}
	if _, err := execLookPath("ufw"); err == nil {
		return nil
	}
	if active := activeForeignFirewallFunc(ctx); active != "" {
		return fmt.Errorf("host firewall %q is already active; Veil manages ufw and installing a second firewall would not reliably open the panel/ACME ports — open the planned ports in %s or disable it, then re-run install", active, active)
	}
	return provisionUFWFunc(ctx)
}

func provisionUFW(ctx context.Context) error {
	managers := []struct {
		name string
		args [][]string
	}{
		{"apt-get", [][]string{{"update"}, {"install", "-y", "ufw"}}},
		{"dnf", [][]string{{"-y", "install", "ufw"}}},
		{"yum", [][]string{{"-y", "install", "ufw"}}},
		{"pacman", [][]string{{"-Sy", "--noconfirm", "ufw"}}},
		{"zypper", [][]string{{"-q", "install", "-y", "ufw"}}},
	}
	for _, m := range managers {
		if _, err := execLookPath(m.name); err != nil {
			continue
		}
		for _, args := range m.args {
			if err := provisionUFWCommandFunc(ctx, m.name, args...); err != nil {
				return fmt.Errorf("install ufw via %s: %w", m.name, err)
			}
		}
		if _, err := execLookPath("ufw"); err == nil {
			return nil
		}
		return fmt.Errorf("ufw installation via %s completed but the ufw command is not in PATH", m.name)
	}
	return fmt.Errorf("the install plan requires ufw to open panel/ACME ports, but ufw is not installed and no supported package manager was found; install ufw (for example `apt-get install -y ufw`) or configure the firewall manually and re-run")
}

func applyRURecommendedInstall(cmd *cobra.Command, profile installer.RURecommendedProfile, opts ruRecommendedInstallOptions) error {
	actualBackupDir := opts.BackupDir
	if !opts.BackupDirSet {
		actualBackupDir = filepath.Join(opts.VarDir, "backups")
	}
	systemdDir := opts.SystemdDir
	if systemdDir == "" {
		systemdDir = defaultSystemdDir
	}
	veilBinary, err := installExecutableFunc()
	if err != nil {
		veilBinary = ""
	}

	// 1. Ensure configuration and state directories exist before running install and writing files
	if err := os.MkdirAll(opts.EtcDir, 0755); err != nil {
		return fmt.Errorf("create etc directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(opts.EtcDir, "certs"), 0700); err != nil {
		return fmt.Errorf("create certs directory: %w", err)
	}
	if err := os.MkdirAll(opts.VarDir, 0755); err != nil {
		return fmt.Errorf("create var directory: %w", err)
	}

	// 1a. In direct mode, the panel endpoint is the public IP. Resolve it once
	// and use it both for the client-link domain and for the LE IP certificate.
	resolvedIP, _ := resolvePublicIPForDirectInstall(cmd.Context(), opts)
	if resolvedIP != nil && profile.PanelAccess == "direct" && profile.Domain == "" {
		profile.Domain = resolvedIP.String()
	}

	// Persist the controlled-CA configuration into veil.env so the running
	// panel keeps rendering Caddy issuers against the same ACME directory
	// (audit #304 controlled-CA leg).
	profile.ACMECAURL = strings.TrimSpace(os.Getenv("VEIL_ACME_CA_URL"))
	profile.ACMECARoot = strings.TrimSpace(os.Getenv("VEIL_ACME_CA_ROOT"))

	// 2. Initialize state.key and encrypted state.json with generated credentials
	resolvedKeyPath := filepath.Join(opts.EtcDir, "state.key")
	resolvedStatePath := filepath.Join(opts.VarDir, "state.json")

	key, err := secrets.LoadOrCreateKey(resolvedKeyPath)
	if err != nil {
		return fmt.Errorf("initialize encryption key: %w", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		return fmt.Errorf("create cipher: %w", err)
	}

	stateExists := false
	if _, err := os.Stat(resolvedStatePath); err == nil {
		stateExists = true
	}

	reusedExistingState := false
	var snapshot model.ManagementSnapshot
	if stateExists {
		// Reuse the existing state.json so an in-place reinstall keeps the admin
		// login and the secret web base path.
		store := managementstate.NewStore(resolvedStatePath, cipher)
		loaded, ok, err := store.Load()
		snapshot = loaded
		if err != nil || !ok {
			// State is present but unreadable (corrupted, or the key no longer
			// matches). Stop instead of silently leaving a panel nobody can log
			// into or overwriting potentially recoverable data.
			return fmt.Errorf(
				"found existing Veil state at %s but could not read it with the encryption key at %s (corrupted state or a mismatched key).\n"+
					"To start fresh: run `sudo veil uninstall` to remove the old state, then reinstall.\n"+
					"To keep the existing data: restore the matching %s before reinstalling",
				resolvedStatePath, resolvedKeyPath, filepath.Base(resolvedKeyPath))
		}
		if snapshot.Settings.WebBasePath != "" {
			// The panel Caddyfile was already rendered with a freshly-generated
			// base path before we knew we'd reuse the existing one. Rewrite it to
			// the reused base path so Caddy routes the same path the panel actually
			// serves (VEIL_WEB_BASE_PATH); otherwise an in-place switch to caddy
			// mode 404s.
			if profile.CaddyJSON != "" && profile.WebBasePath != "" {
				profile.CaddyJSON = strings.ReplaceAll(profile.CaddyJSON, profile.WebBasePath, snapshot.Settings.WebBasePath)
			}
			profile.WebBasePath = snapshot.Settings.WebBasePath
		}
		// Use the first admin user's username
		for _, u := range snapshot.Users {
			if u.Role == "admin" {
				profile.Username = u.Username
				break
			}
		}
		profile.Password = "" // clear it, as we didn't generate a new one
		reusedExistingState = true
		// Direct mode needs a domain for client links. If the existing state
		// was created before auto-fill was implemented, backfill it now.
		if resolvedIP != nil && snapshot.Settings.Domain == "" {
			if _, err := statecommit.Update(statecommit.UpdateOptions{
				StatePath: resolvedStatePath, KeyPath: resolvedKeyPath,
			}, func(current *model.ManagementSnapshot) error {
				if current.Settings.Domain == "" {
					current.Settings.Domain = resolvedIP.String()
				}
				snapshot = *current
				return nil
			}); err != nil {
				return fmt.Errorf("update existing panel state domain: %w", err)
			}
		}
	} else {
		hashed, err := bcrypt.GenerateFromPassword([]byte(profile.Password), 10)
		if err != nil {
			return fmt.Errorf("hash admin password: %w", err)
		}

		defaultState := managementstate.BuildDefaultState(managementstate.DefaultInput{
			PanelListen: profile.PanelListen,
			PanelAccess: profile.PanelAccess,
			WebBasePath: profile.WebBasePath,
			Domain:      profile.Domain,
			Email:       profile.Email,
		})

		initialSnapshot := model.ManagementSnapshot{
			Settings: defaultState.Settings,
			Users: []model.User{
				{
					Username:     profile.Username,
					PasswordHash: string(hashed),
					Role:         "admin",
				},
			},
		}
		managementstate.CompleteSetupForAdmins(&initialSnapshot, time.Now())

		if _, err := statecommit.Save(initialSnapshot, statecommit.Options{StatePath: resolvedStatePath, Cipher: cipher}); err != nil {
			return fmt.Errorf("write initial state.json: %w", err)
		}
	}

	if reusedExistingState && profile.InstallPanelCaddy {
		profile.CaddyJSON = retainCaddyJSONOnReinstall(profile, snapshot, opts.EtcDir)
	}

	// 2a. Open the planned ACME HTTP-01 port (and the rest of the install
	// firewall plan) before requesting the certificate. Fall back to the
	// self-signed certificate already in the profile if issuance fails.
	installPlan, planErr := buildInstallPlan(profile, opts)
	if planErr == nil && len(installPlan.FirewallActions) > 0 {
		if err := installFirewallApplyFunc(installPlan.FirewallActions); err != nil {
			_ = writeAuditInstall(opts.AuditLog, "", false, err.Error(), nil)
			return fmt.Errorf("apply firewall rules: %w", err)
		}
	}
	// 3. Create the veil service account before writing/chowning secrets AND
	// before the ACME IP certificate is issued: fixCertOwnership chgrps the
	// installed key to the veil group, which must already exist (audit #120).
	// installer.Apply chowns generated files to the veil user; that lookup
	// fails on a fresh host if useradd has not run yet.
	if shouldPrepareInstallHost(systemdDir) {
		if err := installPrepareHostFunc(hostaccess.Paths{EtcDir: opts.EtcDir, VarDir: opts.VarDir}); err != nil {
			_ = writeAuditInstall(opts.AuditLog, "", false, err.Error(), nil)
			return fmt.Errorf("prepare panel service account and permissions: %w", err)
		}
	}
	if opts.PanelAccess == "direct" && opts.LEIPCert {
		if err := issueLEIPCertForProfile(cmd.Context(), &profile, opts, resolvedIP); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: could not obtain Let's Encrypt IP certificate: %v\n", err)
			fmt.Fprintln(cmd.ErrOrStderr(), "Falling back to the generated self-signed certificate.")
		}
	}

	// 4. Apply profile configurations (veil.env, caddyfile, systemd unit files, etc.)
	result, err := installApplyFunc(profile, installer.ApplyPaths{
		EtcDir:      opts.EtcDir,
		VarDir:      opts.VarDir,
		SystemdDir:  systemdDir,
		BackupDir:   actualBackupDir,
		VeilBinary:  veilBinary,
		CaddyBinary: opts.CaddyBinary,
	})
	if err != nil {
		_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), nil)
		return err
	}

	if shouldPrepareInstallHost(systemdDir) {
		if err := installSystemdRunFunc(service.SystemdApplyPlan(installer.PanelSystemdUnits(profile))); err != nil {
			_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), result.WrittenFiles)
			return err
		}
		if err := installWaitPanelReadyFunc(cmd, profile, opts); err != nil {
			_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), result.WrittenFiles)
			return fmt.Errorf("panel did not become ready: %w", err)
		}
		// The loopback panel probe does not cover the Caddy leg: a dead
		// veil-caddy.service or a :443 listener that never came up still prints
		// credentials for an unreachable URL (audit #304 — verify the public
		// reverse-proxy route, not only its loopback backend).
		if profile.InstallPanelCaddy {
			if err := installVerifyCaddyRouteFunc(cmd, profile); err != nil {
				_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), result.WrittenFiles)
				return fmt.Errorf("caddy panel proxy did not become ready: %w", err)
			}
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Written files:")
	for _, path := range result.WrittenFiles {
		fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", path)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", resolvedStatePath)
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprint(cmd.OutOrStdout(), installflow.CredentialSummary(profile))
	if reusedExistingState {
		fmt.Fprintf(cmd.OutOrStdout(), "Reused the existing admin login and panel path from %s; your previous password still applies.\n", resolvedStatePath)
		fmt.Fprintf(cmd.OutOrStdout(), "To set a new password: sudo veil admin set --username %s --password 'NEW' --role admin\n", profile.Username)
		fmt.Fprintln(cmd.OutOrStdout(), "To wipe and start fresh instead: run sudo veil uninstall, then reinstall.")
	}
	if err := writeAuditInstall(opts.AuditLog, result.BackupID, true, "", result.WrittenFiles); err != nil {
		return fmt.Errorf("audit log write failed after successful install: %w", err)
	}
	return nil
}

func shouldPrepareInstallHost(systemdDir string) bool {
	return filepath.Clean(systemdDir) == filepath.Clean(defaultSystemdDir)
}

func buildInstallPlan(profile installer.RURecommendedProfile, opts ruRecommendedInstallOptions) (installer.InstallPlan, error) {
	platform := hostenv.CurrentPlatform()
	if platform.OS != "linux" {
		platform.OS = "linux"
	}
	caddyBinary := opts.CaddyBinary
	if profile.InstallPanelCaddy && caddyBinary == "" {
		if path, err := execLookPath("caddy"); err == nil {
			caddyBinary = path
		}
	}
	// The actual panel port is embedded in the rendered profile (e.g.
	// "0.0.0.0:25500"); opts.PanelPort is 0 when a random port was chosen.
	panelPort := opts.PanelPort
	if _, port, err := net.SplitHostPort(profile.PanelListen); err == nil {
		if parsed, parseErr := parsePort(port); parseErr == nil {
			panelPort = parsed
		}
	}
	if profile.InstallPanelCaddy {
		panelPort = 0
	}
	leIPCertPort := 0
	if profile.PanelAccess == "direct" && opts.LEIPCert {
		leIPCertPort = opts.LEIPCertPort
	}
	return installer.BuildInstallPlan(profile, installer.InstallPlanInput{
		Platform:     platform,
		SystemdUnits: installer.PanelSystemdUnits(profile),
		PanelAccess:  profile.PanelAccess,
		PanelPort:    panelPort,
		CaddyBinary:  caddyBinary,
		LEIPCertPort: leIPCertPort,
	})
}

func issueLEIPCertForProfile(ctx context.Context, profile *installer.RURecommendedProfile, opts ruRecommendedInstallOptions, resolvedIP net.IP) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if resolvedIP == nil {
		publicIP := opts.PublicIP
		if publicIP == "" {
			publicIP = "auto"
		}
		var err error
		resolvedIP, err = hostenv.ResolvePublicIP(ctx, publicIP, installPublicIPClient, installPublicIPEndpoints)
		if err != nil {
			return fmt.Errorf("detect public IP: %w", err)
		}
		if resolvedIP == nil {
			return fmt.Errorf("public IP detection returned empty")
		}
	}

	certPath := filepath.Join(opts.EtcDir, "panel", "tls.crt")
	keyPath := filepath.Join(opts.EtcDir, "panel", "tls.key")
	if err := os.MkdirAll(filepath.Dir(certPath), 0o750); err != nil {
		return fmt.Errorf("create panel cert directory: %w", err)
	}
	cert, err := leIPCertIssueFunc(ctx, acmeip.IssueOptions{
		PublicIPv4: resolvedIP.String(),
		HTTPPort:   opts.LEIPCertPort,
		Email:      opts.Email,
		CertPath:   certPath,
		KeyPath:    keyPath,
		// VEIL_ACME_CA_URL points issuance at a controlled ACME CA (e.g. a test
		// CA in CI) instead of Let's Encrypt; VEIL_ACME_INSECURE skips ACME
		// endpoint TLS verification for self-signed test endpoints.
		CAServer: strings.TrimSpace(os.Getenv("VEIL_ACME_CA_URL")),
		Insecure: strings.TrimSpace(os.Getenv("VEIL_ACME_INSECURE")) != "",
	})
	if err != nil {
		return err
	}

	certPEM, err := os.ReadFile(cert.CertPath)
	if err != nil {
		return fmt.Errorf("read issued certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(cert.KeyPath)
	if err != nil {
		return fmt.Errorf("read issued key: %w", err)
	}
	profile.PanelTLSCertPEM = string(certPEM)
	profile.PanelTLSKeyPEM = string(keyPEM)

	// The issued certificate names the public IP, not the configured hostname
	// (audit #299). Point the panel domain — already persisted in state.json —
	// at the covered identity so client links, the printed endpoint, and the
	// readiness check all present a URL the certificate validates for.
	if ipText := resolvedIP.String(); profile.Domain != ipText {
		if err := updateInstalledStateDomain(opts, ipText); err != nil {
			return fmt.Errorf("update panel domain to issued IP identity: %w", err)
		}
		profile.Domain = ipText
	}
	return nil
}

func updateInstalledStateDomain(opts ruRecommendedInstallOptions, domain string) error {
	_, err := statecommit.Update(statecommit.UpdateOptions{
		StatePath: filepath.Join(opts.VarDir, "state.json"),
		KeyPath:   filepath.Join(opts.EtcDir, "state.key"),
	}, func(current *model.ManagementSnapshot) error {
		current.Settings.Domain = domain
		return nil
	})
	return err
}

func parsePort(s string) (int, error) {
	var port int
	_, err := fmt.Sscanf(s, "%d", &port)
	return port, err
}

func resolvePublicIPForDirectInstall(ctx context.Context, opts ruRecommendedInstallOptions) (net.IP, error) {
	if opts.PanelAccess != "direct" {
		return nil, nil
	}
	publicIP := opts.PublicIP
	if publicIP == "" {
		publicIP = "auto"
	}
	return hostenv.ResolvePublicIP(ctx, publicIP, installPublicIPClient, installPublicIPEndpoints)
}

var execLookPath = exec.LookPath

func waitForInstalledPanelReady(cmd *cobra.Command, profile installer.RURecommendedProfile, opts ruRecommendedInstallOptions) error {
	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}
	ctx, cancel := context.WithTimeout(ctx, installPanelReadyTimeout)
	defer cancel()

	certPath := ""
	serverName := ""
	if profile.PanelTLSEnabled {
		certPath = filepath.Join(opts.EtcDir, "panel", "tls.crt")
		serverName = readinessServerName(certPath, profile.Domain)
	}
	contract := statusflow.ContractFromServe(profile.PanelListen, profile.PanelTLSEnabled, profile.WebBasePath, certPath, serverName)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var last error
	for {
		last = probeInstalledPanel(ctx, contract, profile.PanelAuthToken)
		if last == nil {
			if err := helperSocketReady(); err != nil {
				last = err
			} else {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			if last == nil {
				last = ctx.Err()
			}
			return fmt.Errorf("%w; check journalctl -u veil.service veil-helper.socket", last)
		case <-ticker.C:
		}
	}
}

// readinessServerName picks a TLS identity the installed certificate actually
// covers for the local post-install health probe (audit #299). profile.Domain
// is panel metadata: in direct mode with an issued IP certificate the cert
// names the public IP, not the domain, and a self-signed cert generated before
// public-IP detection does not contain the backfilled domain at all. Verifying
// against profile.Domain then fails a healthy panel. Identities valid for the
// loopback probe are preferred, then any SAN on the cert.
func readinessServerName(certPath, configuredDomain string) string {
	fallback := configuredDomain
	if fallback == "" {
		fallback = "localhost"
	}
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		return fallback
	}
	var cert *x509.Certificate
	rest := pemBytes
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		if parsed, parseErr := x509.ParseCertificate(block.Bytes); parseErr == nil {
			cert = parsed
			break
		}
	}
	if cert == nil {
		return fallback
	}
	candidates := []string{"localhost", "127.0.0.1", "::1"}
	if ip := net.ParseIP(strings.TrimSpace(configuredDomain)); ip != nil {
		candidates = append(candidates, ip.String())
	}
	for _, ipSAN := range cert.IPAddresses {
		candidates = append(candidates, ipSAN.String())
	}
	for _, candidate := range candidates {
		if cert.VerifyHostname(candidate) == nil {
			return candidate
		}
	}
	if configuredDomain != "" && cert.VerifyHostname(configuredDomain) == nil {
		return configuredDomain
	}
	if len(cert.DNSNames) > 0 {
		return cert.DNSNames[0]
	}
	return fallback
}

func helperSocketReady() error {
	if _, err := os.Lstat(installHelperSocketPath); err != nil {
		return fmt.Errorf("helper socket %s: %w", installHelperSocketPath, err)
	}
	return nil
}

func probeInstalledPanel(ctx context.Context, contract statusflow.ContainerHealthContract, token string) error {
	url, err := statusflow.LocalHealthURL(contract)
	if err != nil {
		return err
	}
	client, err := statusflow.HealthHTTPClient(contract)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("X-Veil-Token", token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("panel healthz %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("panel healthz %s: %s", url, resp.Status)
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "application/json") {
		return fmt.Errorf("panel healthz %s is not a Veil instance (content-type %q)", url, resp.Header.Get("Content-Type"))
	}
	var payload struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Status != "ok" {
		return fmt.Errorf("panel healthz %s is not a Veil instance", url)
	}
	return nil
}

var installProbeCaddyCapabilities = caddycapabilities.Probe

// installVerifyCaddyRouteFunc is a seam so install tests can stub the live
// service/socket checks without a real systemd host.
var installVerifyCaddyRouteFunc = verifyInstalledCaddyRoute

// installCaddyUnitActiveFunc reports whether veil-caddy.service is active.
var installCaddyUnitActiveFunc = func(ctx context.Context) error {
	return exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "veil-caddy.service").Run()
}

// verifyInstalledCaddyRoute proves the public reverse-proxy leg after a caddy
// install: the unit is running, something is listening on TCP :443, and TLS
// answers for the configured domain. A still-pending ACME certificate or a
// DNS record pointing elsewhere is reported as an actionable warning instead
// of a silent success (audit #304).
func verifyInstalledCaddyRoute(cmd *cobra.Command, profile installer.RURecommendedProfile) error {
	ctx := context.Background()
	if cmd != nil && cmd.Context() != nil {
		ctx = cmd.Context()
	}
	deadline, cancel := context.WithTimeout(ctx, installPanelReadyTimeout)
	defer cancel()

	// veil-caddy.service may still be in a restart loop right after the apply;
	// give it the same readiness window as the panel before declaring failure.
	var lastErr error
	for {
		if err := installCaddyUnitActiveFunc(deadline); err == nil {
			conn, dialErr := caddyRouteDialer(deadline, "tcp", "127.0.0.1:443")
			if dialErr == nil {
				conn.Close()
				break
			}
			lastErr = fmt.Errorf("veil-caddy.service is active but nothing listens on 127.0.0.1:443: %w", dialErr)
		} else {
			lastErr = fmt.Errorf("veil-caddy.service is not active: %w", err)
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("%w; check journalctl -u veil-caddy.service", lastErr)
		case <-time.After(250 * time.Millisecond):
		}
	}

	domain := strings.TrimSpace(profile.Domain)
	if domain == "" {
		return nil
	}
	warnf := func(format string, args ...any) {
		if cmd != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "WARNING: "+format+"\n", args...)
		}
	}

	// TLS probe through the public route: SNI=<domain> against loopback :443.
	// A pending/self-signed cert fails verification and lands in the warn path,
	// which is exactly the signal that ACME issuance is still in flight.
	base := profile.WebBasePath
	if base == "" {
		base = "/"
	}
	if probeErr := caddyPublicRouteProbeFunc(ctx, domain, base); probeErr != nil {
		warnf("the panel route https://%s%s is not serving TLS yet: %v\n  If this persists, check that %s resolves to this host and that TCP :443 is reachable from the internet (cloud firewall/security group), and see journalctl -u veil-caddy.service for ACME errors.", domain, base, probeErr, domain)
	}

	// DNS advisory: a domain that does not resolve to this host's public IP
	// makes the panel unreachable no matter how healthy caddy is locally.
	if ips, err := caddyDomainLookupFunc(ctx, domain); err == nil && len(ips) > 0 {
		if publicIP, err := installPublicIPResolveFunc(ctx); err == nil && publicIP != nil {
			match := false
			for _, ip := range ips {
				if ip == publicIP.String() {
					match = true
					break
				}
			}
			if !match {
				warnf("domain %s resolves to %s but this host's public IP is %s — the panel will be unreachable until DNS is updated (or an upstream proxy forwards the traffic).", domain, strings.Join(ips, ", "), publicIP.String())
			}
		}
	} else if err != nil {
		warnf("domain %s does not resolve (%v) — the panel will be unreachable until DNS is configured.", domain, err)
	}
	return nil
}

var caddyRouteDialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
	return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, addr)
}

// caddyPublicRouteProbeFunc issues an HTTPS request through caddy's public
// :443 listener with SNI/Host set to the panel domain — proving the public
// reverse-proxy route, not only the loopback backend.
var caddyPublicRouteProbeFunc = func(ctx context.Context, domain, basePath string) error {
	probeCtx, probeCancel := context.WithTimeout(ctx, 10*time.Second)
	defer probeCancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, "https://127.0.0.1:443"+basePath, nil)
	if err != nil {
		return err
	}
	req.Host = domain
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if caRoot := strings.TrimSpace(os.Getenv("VEIL_ACME_CA_ROOT")); caRoot != "" {
		if pemBody, readErr := os.ReadFile(caRoot); readErr == nil {
			roots.AppendCertsFromPEM(pemBody)
		}
	}
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{ServerName: domain, RootCAs: roots, MinVersion: tls.VersionTLS12},
	}}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

var caddyDomainLookupFunc = func(ctx context.Context, domain string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, domain)
}

var installPublicIPResolveFunc = func(ctx context.Context) (net.IP, error) {
	return hostenv.ResolvePublicIP(ctx, "auto", installPublicIPClient, installPublicIPEndpoints)
}

func retainCaddyJSONOnReinstall(profile installer.RURecommendedProfile, snapshot model.ManagementSnapshot, etcDir string) string {
	livePath := filepath.Join(etcDir, "generated", "caddy", "config.json")
	live := ""
	if body, err := os.ReadFile(livePath); err == nil {
		live = strings.TrimSpace(string(body))
	}
	if rendered, err := renderCaddyJSONFromSnapshot(snapshot, profile); err == nil && strings.TrimSpace(rendered) != "" {
		if len(snapshot.Inbounds) > 0 || live == "" {
			return rendered
		}
	}
	if live != "" {
		return live
	}
	return profile.CaddyJSON
}

func renderCaddyJSONFromSnapshot(snapshot model.ManagementSnapshot, profile installer.RURecommendedProfile) (string, error) {
	settings := snapshot.Settings
	// This render only runs when the operator just requested caddy mode — the
	// snapshot still carries the previous install's settings. Trusting them
	// verbatim renders the old mode's config (e.g. PanelAccess=direct produces
	// no panel server at all): caddy then starts, reports "serving initial
	// configuration", and binds nothing on :443 (audit #304 caddy leg).
	settings.PanelAccess = "caddy"
	if profile.WebBasePath != "" {
		settings.WebBasePath = profile.WebBasePath
	}
	if profile.Domain != "" {
		settings.Domain = profile.Domain
		settings.PanelDomain = profile.Domain
	}
	if settings.PanelDomain == "" {
		settings.PanelDomain = settings.Domain
	}
	if profile.Email != "" {
		settings.Email = profile.Email
		settings.PanelEmail = profile.Email
	}
	if settings.PanelEmail == "" {
		settings.PanelEmail = settings.Email
	}
	if profile.PanelListen != "" {
		settings.PanelListen = profile.PanelListen
	}
	// Caddy always terminates the panel on :443 via tls-alpn-01 — mirroring
	// panelaccess.Profile.Build; a stale direct-mode snapshot carries the old
	// panel port and an empty/http-01 challenge mode.
	settings.PanelPublicPort = 443
	settings.AcmeChallengeMode = "tls-alpn-01"
	plan, _, _, err := caddyassembly.BuildFinalRenderPlan(settings, snapshot.Inbounds)
	if err != nil {
		return "", err
	}
	caps, err := installProbeCaddyCapabilities("")
	if err != nil {
		if !caddycapabilities.IsMissingBinary(err) {
			return "", err
		}
		caps = caddycapabilities.CaddyCapabilities{}
	}
	if snapshotHasNaiveInbound(snapshot) {
		caps.ForwardProxy = true
	}
	body, err := renderer.RenderCaddyJSON(plan, caps)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func snapshotHasNaiveInbound(snapshot model.ManagementSnapshot) bool {
	for _, inbound := range snapshot.Inbounds {
		if inbound.Enabled && inbound.Protocol == "naiveproxy" {
			return true
		}
	}
	return false
}
